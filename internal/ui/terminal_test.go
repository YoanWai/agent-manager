package ui

import (
	"slices"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func terminalSession(t *testing.T, m *Model) store.Session {
	t.Helper()
	for _, sess := range m.sessions {
		if sess.Tool == terminalTool {
			return sess
		}
	}
	t.Fatalf("no terminal session among %v", m.sessions)
	return store.Session{}
}

func TestOpenTerminalSpawnsShellInSelectedGroup(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	_, cmd := m.openTerminal()
	if m.errBar.text != "" {
		t.Fatalf("terminal spawn reported %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)

	sess := terminalSession(t, m)
	if sess.Group != "backend" {
		t.Fatalf("terminal group = %q, want backend", sess.Group)
	}
	if sess.Cwd != dir {
		t.Fatalf("terminal cwd = %q, want %q", sess.Cwd, dir)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("terminal spawn left no tmux session")
	}
	if row, ok := m.selected(); !ok || row.ID != sess.ID {
		t.Fatalf("cursor should land on the new terminal, selected = %+v", row)
	}
}

// A terminal opened on a session row follows that session's directory, so
// the shell lands where the agent works rather than at the group default.
func TestOpenTerminalOnSessionRowUsesItsDirectory(t *testing.T) {
	m := buildModel(t)
	groupDir, sessionDir := t.TempDir(), t.TempDir()
	if err := m.store.CreateGroup("backend", groupDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent", sessionDir, "backend")
	m.selectSessionRow(t, "agent")

	_, cmd := m.openTerminal()
	m.applyCmd(t, cmd)

	sess := terminalSession(t, m)
	if want := resolved(t, sessionDir); sess.Cwd != want {
		t.Fatalf("terminal cwd = %q, want the session's %q", sess.Cwd, want)
	}
	if sess.Group != "backend" {
		t.Fatalf("terminal group = %q, want backend", sess.Group)
	}
}

// Killing frees the shell like any other session, and revive brings it
// back on the tool's empty command rather than erroring on a missing CLI.
func TestTerminalSessionRevives(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	_, cmd := m.openTerminal()
	m.applyCmd(t, cmd)

	sess := terminalSession(t, m)
	if err := m.killSession(sess); err != nil {
		t.Fatalf("kill terminal: %v", err)
	}
	sess.Status = status.Dead
	if err := m.reviveSession(sess); err != nil {
		t.Fatalf("revive terminal: %v", err)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("revived terminal has no tmux session")
	}
}

// The terminal tab is a shell, not a CLI to spawn agents with: it stays out
// of every picker, but a terminal session keeps it on rename so saving
// cannot silently turn the shell into an agent.
func TestTerminalToolStaysOutOfPickers(t *testing.T) {
	m := buildModel(t)
	if slices.Contains(m.enabledToolNames(), terminalTool) {
		t.Fatalf("terminal should not be offered as a CLI: %v", m.enabledToolNames())
	}
	m.applyCmd(t, m.refreshCmd())
	_, cmd := m.openTerminal()
	m.applyCmd(t, cmd)

	m.selectSessionRow(t, terminalSession(t, m).Name)
	m.openRename()
	if got := m.renameTool(); got != terminalTool {
		t.Fatalf("rename tool = %q, want %q", got, terminalTool)
	}
}
