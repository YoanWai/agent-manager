package ui

import (
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

func renameOpen(m *Model) bool {
	_, ok := m.renaming()
	return ok
}

// openRename is the rename on the stack; a test that edits one without
// opening it fails here, not on a nil.
func openRename(m *Model) *uidialogs.RenameSurface {
	s, ok := m.renaming()
	if !ok {
		panic("no rename is open")
	}
	return s
}

func openRenameFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.openRename()
	if !renameOpen(m) {
		t.Fatalf("test setup did not open the rename, err %q", m.errBar.text)
	}
	return m
}

// The name is typed into its row, so the surface paints no card, keeps its
// draft through a stray click and leaves the list where it is.
func TestRenameIsARowNotACard(t *testing.T) {
	m := openRenameFor(t)
	if mustTop(t, m).Dismissible() {
		t.Fatal("a rename holds a typed draft and must not close on a stray click")
	}
	m.prepareFrame()
	if m.layout.card != (cardRect{}) {
		t.Fatalf("rename painted a card: %+v", m.layout.card)
	}
	if m.cardOpen() != true {
		// cardOpen follows the stack; routeMouse still holds the list still.
		t.Fatal("the stack's top is not a card")
	}
	if m = leftClick(m, 0, 0); !renameOpen(m) {
		t.Fatal("a click closed the rename draft")
	}
}

func TestRenameEscClosesAndCtrlCQuits(t *testing.T) {
	m := openRenameFor(t)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if renameOpen(m) || m.effects.quitting || !m.onList() {
		t.Fatalf("esc: open=%t quitting=%t screen = %v", renameOpen(m), m.effects.quitting, m.screenID())
	}
	m = openRenameFor(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.effects.quitting {
		t.Fatal("ctrl+c over the list did not quit")
	}
}

// A completion for a rename that was popped, or for one reopened since, is
// dropped.
func TestStaleRenameCompletionIsDropped(t *testing.T) {
	m := openRenameFor(t)
	gen := m.gens.dialog
	name := openRename(m).Name()
	m.Pop()
	m.openRename()
	m.gens.dialog++
	job := &effectJob{request: renameRequest{kind: renameSession, sessID: openRename(m).SessionID(), name: name, gen: gen}}
	m.applyRenameEffect(job, renameEffectResult{}, nil)
	if !renameOpen(m) {
		t.Fatal("a completion from an older rename closed the newer one")
	}
}
