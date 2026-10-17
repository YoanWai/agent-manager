package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func requireFocusDriver(t *testing.T) *tmux.Driver {
	t.Helper()
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("NewWithSocket: %v", err)
	}
	return driver
}

// skipControlMode skips a test that needs the watcher's control client up:
// on Windows OpenControl refuses by design and previews poll instead.
func skipControlMode(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the manager does not use psmux control mode")
	}
}

// The watcher pushes a fresh preview when the pane paints, with no tick
// asking for it: pushed over the control client, or polled where there is
// none.
func TestFocusWatchPushesPaneUpdates(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "focus" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.SetFocus(id)

	// The watcher seeds one capture on open.
	waitFocusPreview(t, msgs, id, "")

	if err := driver.SendText(id, "echo focus-watch-ping"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitFocusPreview(t, msgs, id, "focus-watch-ping")
}

func waitFocusPreview(t *testing.T, msgs <-chan tea.Msg, id, contains string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(focusPreviewMsg)
			if !ok || preview.sessID != id {
				continue
			}
			if strings.Contains(preview.preview, contains) {
				return
			}
		case <-deadline:
			t.Fatalf("no focusPreviewMsg containing %q", contains)
		}
	}
}

// A client that dies under the watcher is reported, not swallowed: the
// preview silently dropping to the poll cadence reads as the manager
// lagging, with nothing on screen saying why. A polled watcher's capture
// failing is the same loss.
func TestFocusWatchReportsALostClient(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "lost" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.SetFocus(id)
	waitFocusPreview(t, msgs, id, "")

	// Killing the session pulls the pane out from under the watcher, which
	// is one of the ways the client goes away in use.
	driver.Kill(id)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if failure, ok := msg.(errMsg); ok {
				if !strings.Contains(failure.err.Error(), "preview") {
					t.Fatalf("lost client reported as %q", failure.err)
				}
				return
			}
		case <-deadline:
			t.Fatal("lost control client was never reported")
		}
	}
}
