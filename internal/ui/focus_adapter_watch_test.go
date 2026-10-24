package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
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
			preview, ok := msg.(focusruntime.FrameMsg)
			if !ok || preview.SessionID != id {
				continue
			}
			if strings.Contains(preview.Preview, contains) {
				return
			}
		case <-deadline:
			t.Fatalf("no focusruntime.FrameMsg containing %q", contains)
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

func seedLifecycleWatcher(t *testing.T, m *Model, id string) {
	t.Helper()
	m.focus.runtime.Watch = newFocusWatch(m.services.tmux, func(tea.Msg) {})
	t.Cleanup(m.focus.runtime.Watch.Close)
	m.focus.runtime.Watch.SetFocus(id)
}

func TestFailedArchiveRestoresSurvivingSelectionWatcher(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	// Archive snapshots only a pane that has drawn; a fresh psmux pane can
	// still be blank, and then no write fails.
	waitForPreview(t, m)
	sess := railSelectedSession(m)
	seedLifecycleWatcher(t, m, sess.ID)
	m.services.setSnapshot = func(string, string) error { return errors.New("disk full") }
	m.archiveSelected()
	if err := m.archiveConfirmed(); err == nil {
		t.Fatal("expected snapshot failure")
	}
	if got := m.focus.runtime.Watch.Watching(); got != sess.ID {
		t.Fatalf("watching=%q, want surviving %q", got, sess.ID)
	}
}

func TestFailedChildDeleteRestoresSurvivingParentWatcher(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "parent", t.TempDir(), "")
	m.selectSessionRow(t, "parent")
	parent := railSelectedSession(m)
	seedLifecycleWatcher(t, m, parent.ID)
	child := store.Session{ID: "cafe", ParentID: parent.ID}
	path := m.services.hooks.StatusFile(child.ID)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "block"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m.openConfirm(confirmTarget{sessions: []store.Session{parent, child}, action: uidialogs.ConfirmDelete})
	if _, err := m.deleteConfirmed(); err == nil {
		t.Fatal("expected child cleanup failure")
	}
	if got := m.focus.runtime.Watch.Watching(); got != parent.ID {
		t.Fatalf("watching=%q, want surviving %q", got, parent.ID)
	}
}
