package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func seedLifecycleWatcher(t *testing.T, m *Model, id string) {
	t.Helper()
	m.focusRuntime.watch = &focusWatch{driver: m.services.tmux, id: id, send: func(tea.Msg) {}}
	t.Cleanup(m.focusRuntime.watch.Close)
}

func TestFailedArchiveRestoresSurvivingSelectionWatcher(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess := railSelectedSession(m)
	seedLifecycleWatcher(t, m, sess.ID)
	m.services.setSnapshot = func(string, string) error { return errors.New("disk full") }
	m.archiveSelected()
	if err := m.archiveConfirmed(); err == nil {
		t.Fatal("expected snapshot failure")
	}
	if got := m.focusRuntime.watch.watching(); got != sess.ID {
		t.Fatalf("watching=%q, want surviving %q", got, sess.ID)
	}
}

func TestFailedChildDeleteRestoresSurvivingParentWatcher(t *testing.T) {
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
	m.confirm = confirmTarget{sessions: []store.Session{parent, child}, action: actionDelete}
	if _, err := m.deleteConfirmed(); err == nil {
		t.Fatal("expected child cleanup failure")
	}
	if got := m.focusRuntime.watch.watching(); got != parent.ID {
		t.Fatalf("watching=%q, want surviving %q", got, parent.ID)
	}
}
