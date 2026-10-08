package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"testing"
)

func TestSessionDirectoryUsesObservedPane(t *testing.T) {
	sess := store.Session{ID: "session", Cwd: "/launch"}
	m := &Model{}
	m.workspace.panes = map[string]tmux.Pane{sess.ID: {Path: "/live directory "}}
	if got := m.sessionDir(sess); got != "/live directory " {
		t.Fatalf("directory = %q", got)
	}
}

func TestSessionDirectoryFallsBackAfterPaneDisappears(t *testing.T) {
	sess := store.Session{ID: "session", Cwd: "/launch"}
	if got := (&Model{}).sessionDir(sess); got != sess.Cwd {
		t.Fatalf("directory = %q", got)
	}
}
