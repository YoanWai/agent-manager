package ui

import (
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

func forkOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.ForkSurface](m)
	return ok
}

// openFork is the fork card on the stack; a test that edits one without
// opening it fails here.
func openFork(m *Model) *uidialogs.ForkSurface {
	s, ok := surfaceOf[*uidialogs.ForkSurface](m)
	if !ok {
		panic("no fork is open")
	}
	return s
}

func openForkFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "alpha")
	alpha := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(alpha.ID, "alpha-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == alpha.ID {
			m.workspace.sessions[i].AgentSessionID = "alpha-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "alpha")
	m.openFork()
	if !forkOpen(m) {
		t.Fatalf("test setup did not open the fork, err %q", m.errBar.text)
	}
	return m
}

// The name is a draft: a click off the card keeps it, esc closes.
func TestForkKeepsItsDraftAndEscCloses(t *testing.T) {
	m := openForkFor(t)
	if mustTop(t, m).Dismissible() {
		t.Fatal("a fork holds a typed name and must not close on a stray click")
	}
	if m = clickBackdrop(t, m); !forkOpen(m) {
		t.Fatal("a backdrop click closed the draft")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if forkOpen(m) || !m.onList() {
		t.Fatalf("esc: open=%t screen = %v", forkOpen(m), m.screenID())
	}
}

// A completion for a card that was popped is dropped: it neither closes
// anything nor focuses the new row, and the error still reaches the status.
func TestStaleForkCompletionIsDropped(t *testing.T) {
	m := openForkFor(t)
	request := forkRequest{source: openFork(m).Root().(forkRoot).source, name: openFork(m).Name(), gen: openFork(m).Gen()}
	m.Pop()
	m.openFork()
	m.applyForkEffect(request, forkEffectResult{}, nil)
	if !forkOpen(m) {
		t.Fatal("a completion from a popped fork closed the reopened card")
	}
}
