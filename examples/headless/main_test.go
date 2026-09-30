package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
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
