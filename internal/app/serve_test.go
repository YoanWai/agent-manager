package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

type recordedStart struct {
	calls   int
	args    []string
	logPath string
}

func (r *recordedStart) start(args []string, logPath string) (int, error) {
	r.calls++
	r.args, r.logPath = args, logPath
	return 4242, nil
}

func stampHeartbeat(t *testing.T, profileDir string, at time.Time) {
	t.Helper()
	st, err := store.Open(filepath.Join(profileDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.ClaimPoller("elsewhere", at); err != nil {
		t.Fatalf("ClaimPoller: %v", err)
	}
}

func TestServeBackgroundLeavesAnAwakeManagerAlone(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	stampHeartbeat(t, dir, now)
	starter := &recordedStart{}
	got, err := ServeBackground(dir, now, starter.start)
	if err != nil {
		t.Fatalf("ServeBackground: %v", err)
	}
	if got.Started || got.PID != 0 || starter.calls != 0 {
		t.Fatalf("an awake manager got a second one: %+v, starts=%d", got, starter.calls)
	}
}

func TestServeBackgroundStartsServeWhenNoManagerIsAwake(t *testing.T) {
	for name, stamp := range map[string]func(t *testing.T, dir string, now time.Time){
		"never ran": func(*testing.T, string, time.Time) {},
		"stopped": func(t *testing.T, dir string, now time.Time) {
			stampHeartbeat(t, dir, now.Add(-store.PollerHeartbeatStale-time.Second))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "profile")
			now := time.Now()
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			stamp(t, dir, now)
			starter := &recordedStart{}
			got, err := ServeBackground(dir, now, starter.start)
			if err != nil {
				t.Fatalf("ServeBackground: %v", err)
			}
			if !got.Started || got.PID != 4242 {
				t.Fatalf("result = %+v", got)
			}
			if starter.calls != 1 || !slices.Equal(starter.args, []string{"serve"}) || starter.logPath != filepath.Join(dir, ServeLog) {
				t.Fatalf("started %d times with %v logging to %q", starter.calls, starter.args, starter.logPath)
			}
		})
	}
}

func TestServeBackgroundReportsAFailedStart(t *testing.T) {
	failure := errors.New("no fork for you")
	_, err := ServeBackground(t.TempDir(), time.Now(), func([]string, string) (int, error) { return 0, failure })
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the starter's", err)
	}
}

func TestServeStopsWhenItsContextEndsAfterPolling(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := tmux.NewWithSocket("amservetest-" + uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	if err := driver.Create(uuid.NewString()[:8], t.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("start the test server: %v", err)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("tmux", "-L", driver.SocketName(), "kill-server").CombinedOutput(); err != nil {
			t.Errorf("kill test tmux server: %v: %s", err, strings.TrimSpace(string(out)))
		}
		_ = os.Remove(driver.SocketPath())
	})
	dir := t.TempDir()
	local, err := OpenLocal(dir, driver)
	if err != nil {
		t.Fatalf("OpenLocal: %v", err)
	}
	defer local.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		local.Serve(ctx, errs)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		awake, err := local.Runtime.Store.ManagerAwake(time.Now())
		if err != nil {
			t.Fatalf("ManagerAwake: %v", err)
		}
		if awake {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("serve never ran a poll step")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serve kept running after its context ended")
	}
	if errs.Len() != 0 {
		t.Fatalf("serve reported errors: %s", errs.String())
	}
}
