package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"path/filepath"
)

type cancelWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.cancel()
	return n, err
}

func TestHeadlessObservationUsesRuntimeWithoutUI(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := tmux.NewWithSocket(fmt.Sprintf("am-poc-headless-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := driver.Create("am-poc-anchor", dir, "exec sleep 30", nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { driver.Kill("am-poc-anchor") })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output := &cancelWriter{cancel: cancel}
	if err := observe(ctx, dir, driver, output); err != nil {
		t.Fatal(err)
	}
	var got observation
	if err := json.NewDecoder(&output.Buffer).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ListedAt.IsZero() || got.Sessions != 0 {
		t.Fatalf("observation=%+v", got)
	}
}

// The helper runs the actual observation/runtime loop in a separate process.
func TestHeadlessProcessHelper(t *testing.T) {
	if os.Getenv("AM_HEADLESS_TEST_CHILD") != "1" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	driver, err := tmux.NewWithSocket(os.Getenv("AM_HEADLESS_TEST_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	if err := observe(ctx, os.Getenv("AM_HEADLESS_TEST_PROFILE"), driver, os.Stdout); err != nil {
		t.Fatal(err)
	}
}

func TestHeadlessProcessProducesObservationAndStopsCleanly(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("am-poc-child-%d", time.Now().UnixNano())
	driver, err := tmux.NewWithSocket(socket)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := driver.Create("am-poc-anchor", dir, "exec sleep 30", nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { driver.Kill("am-poc-anchor") })
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(store.Session{ID: "am-poc-target", Name: "worker", Tool: "claude", Cwd: dir, Status: "dead"}); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if _, err := st.Enqueue(store.InboxMessage{SessionID: "am-poc-target", SenderID: "sender", Body: "queued instruction", Fingerprint: "queued instruction", SentAt: time.Now()}, store.DefaultInboxLimits); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestHeadlessProcessHelper$")
	child.Env = append(os.Environ(), "AM_HEADLESS_TEST_CHILD=1", "AM_HEADLESS_TEST_SOCKET="+socket, "AM_HEADLESS_TEST_PROFILE="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	var got observation
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("no child observation: %v %s", scanner.Err(), stderr.String())
	}
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ListedAt.IsZero() || got.Sessions != 1 || len(got.Backlog) != 1 || got.Backlog[0] != "worker has 1 queued messages" {
		t.Fatalf("observation: %+v", got)
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("child shutdown: %v %s", err, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("shutdown reached deadline: %v", ctx.Err())
	}
}
