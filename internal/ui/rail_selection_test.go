package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestCursorWrapsAroundTheList(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "first", dir, "")
	createSession(t, m, "second", dir, "")

	m.rail.cursor = 0
	m.moveCursor(-1)
	if m.rail.cursor != len(m.rail.rows)-1 {
		t.Fatalf("up from the top should wrap to the bottom, cursor = %d", m.rail.cursor)
	}
	m.moveCursor(1)
	if m.rail.cursor != 0 {
		t.Fatalf("down from the bottom should wrap to the top, cursor = %d", m.rail.cursor)
	}

	m.rail.rows = nil
	m.rail.cursor = 0
	m.moveCursor(1)
	if m.rail.cursor != 0 {
		t.Fatalf("empty list should leave the cursor alone, cursor = %d", m.rail.cursor)
	}
}

// Right steps into the row under the cursor: a session is focused, and a
// collapsed group opens without the toggle closing an open one.
func TestRightStepsIntoTheRow(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("grouped", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "stepin", t.TempDir(), "grouped")
	m.selectGroupRow(t, "grouped")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if !m.rail.collapsed["grouped"] {
		t.Fatal("left did not close the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.rail.collapsed["grouped"] {
		t.Fatal("right did not open the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.rail.collapsed["grouped"] {
		t.Fatal("a second right closed the group it had opened")
	}

	m.selectSessionRow(t, "stepin")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("right did not focus the session, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}
