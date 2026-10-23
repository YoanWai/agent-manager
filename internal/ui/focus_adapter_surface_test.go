package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// onList is the list with no session focused over it.
func (m *Model) onList() bool { return m.base == baseList && !m.focused() }

// Enter focuses the selected session by mounting the surface, which hands
// the keyboard to the agent; the detach key pops it.
func TestFocusIsASurface(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "focus-surface", t.TempDir(), "")
	m.selectSessionRow(t, "focus-surface")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() || !m.agentOwnsKeyboard() {
		t.Fatalf("enter did not focus: focused=%t agent=%t", m.focused(), m.agentOwnsKeyboard())
	}
	if m.cardOpen() || mustTop(t, m).Dismissible() {
		t.Fatal("focus is no card and holds the agent's keys")
	}
	m.leaveFocus()
	if m.focused() || len(m.surfaces) != 0 {
		t.Fatal("leaving focus left the surface mounted")
	}
}
