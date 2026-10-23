package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func moveOpen(m *Model) bool {
	_, ok := surfaceOf[*moveSurface](m)
	return ok
}

// movingSession is the session the open card is placing.
func movingSession(m *Model) string {
	if s, ok := surfaceOf[*moveSurface](m); ok {
		return s.SessionID()
	}
	return ""
}

func openMoveFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if !moveOpen(m) {
		t.Fatalf("test setup did not open the card, err %q", m.errBar.text)
	}
	return m
}

func TestMoveClosesOnEscAndBackdrop(t *testing.T) {
	for name, close := range map[string]func(*Model) *Model{
		"esc":      func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return m },
		"backdrop": func(m *Model) *Model { return clickBackdrop(t, m) },
	} {
		m := openMoveFor(t)
		if !mustTop(t, m).Dismissible() {
			t.Fatalf("%s: the move card holds no draft and must be dismissible", name)
		}
		m = close(m)
		if moveOpen(m) || m.effects.quitting || !m.onList() {
			t.Errorf("%s: open=%t quitting=%t screen = %v, want the card closed and the manager running", name, moveOpen(m), m.effects.quitting, m.screenID())
		}
	}
}

// A close follow-up from a card that was popped, or replaced by a newer
// one, leaves the screen alone.
func TestStaleMoveCloseIsDropped(t *testing.T) {
	m := openMoveFor(t)
	stale := moveDialogClose{gen: m.gens.dialog}
	m.Pop()
	m.applyMoveDialogClose(stale)
	if moveOpen(m) || len(m.surfaces) != 0 {
		t.Fatal("a stale close touched the stack")
	}
	m.openMove()
	m.applyMoveDialogClose(stale)
	if !moveOpen(m) {
		t.Fatal("a close from an older card closed the newer one")
	}
	m.applyMoveDialogClose(moveDialogClose{gen: m.gens.dialog})
	if moveOpen(m) {
		t.Fatal("the close of the open card did not close it")
	}
}
