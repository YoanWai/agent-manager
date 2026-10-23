package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// focusWithSavedList is a focused session with the saved prompts list on
// the stack over it.
func focusWithSavedList(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	createSession(t, m, "surface", t.TempDir(), "")
	m.selectSessionRow(t, "surface")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("enter did not focus, screen = %v", m.screenID())
	}
	s := newFocusSaved(0, m.spawnStyles())
	s.list.Load([]spawn.Saved{{Name: "Role", Text: "be brief"}}, nil)
	m.push(s)
	return m
}

// The surface on top owns the keyboard: the manager reads keys there, even
// over a focused session whose pane would otherwise take them.
func TestTopSurfaceOwnsTheKeyboard(t *testing.T) {
	m := focusWithSavedList(t)
	if m.agentOwnsKeyboard() {
		t.Fatal("the agent owns the keyboard under an open list")
	}
	m.pop()
	if !m.agentOwnsKeyboard() {
		t.Fatal("focus with no surface did not hand the keyboard to the agent")
	}
}

// A key typed over the list filters the list and never reaches the pane.
func TestSurfaceTakesKeysBeforeThePane(t *testing.T) {
	m := focusWithSavedList(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	if m.effects.input.active != nil || len(m.effects.input.pending) > 0 {
		t.Fatal("a key typed over the list reached the agent")
	}
	if !m.savedListOpen() {
		t.Fatal("typing closed the list")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.savedListOpen() || !m.focused() {
		t.Fatalf("esc: list open=%t screen = %v", m.savedListOpen(), m.screenID())
	}
}

// The frame paints the surface over the base, the footer leads with its
// keys, and the mouse treats it as the open card.
func TestSurfaceOwnsFrameFooterAndMouse(t *testing.T) {
	m := focusWithSavedList(t)
	m.prepareFrame()
	if !strings.Contains(m.View(), "saved prompts") {
		t.Fatal("the frame does not show the list")
	}
	footer := ansi.Strip(m.viewFooter())
	if filter, back := strings.Index(footer, "filter"), strings.Index(footer, "back"); filter < 0 || back < 0 || filter > back {
		t.Fatalf("footer = %q, want the list's keys ahead of focus's own", footer)
	}
	if !m.cardOpen() {
		t.Fatal("the mouse does not see the list as the open card")
	}
}

// Leaving focus takes every surface over it down too.
func TestLeavingFocusClearsItsSurfaces(t *testing.T) {
	m := focusWithSavedList(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !m.onList() || len(m.surfaces) != 0 {
		t.Fatalf("screen = %v surfaces=%d", m.screenID(), len(m.surfaces))
	}
}

// The IME anchor follows whoever reads the keys: the list's filter while
// it is up, not the pane's caret behind it.
func TestIMEAnchorFollowsTheInputOwner(t *testing.T) {
	m := focusWithSavedList(t)
	m.focus.runtime.imeCursor = &cursorAnchor{}
	frame := "ab" + cursorAnchorMarker + "c"
	if got := m.syncCursorAnchor(frame); got != "abc" {
		t.Fatalf("frame = %q", got)
	}
	col, row, ok := m.focus.runtime.imeCursor.get()
	if !ok || col != 3 || row != 1 {
		t.Fatalf("anchor = (%d, %d, %t), want the filter's caret at (3, 1)", col, row, ok)
	}
}

// A click off a dismissible card closes it the way esc does, through the
// same router: the surface says it is dismissible, mouse.go does not guess.
func TestClickOffADismissibleSurfaceClosesIt(t *testing.T) {
	m := focusWithSavedList(t)
	m.prepareFrame()
	press := tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	release := tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
	m.Update(press)
	m.Update(release)
	if m.savedListOpen() || !m.focused() {
		t.Fatalf("list open=%t screen = %v", m.savedListOpen(), m.screenID())
	}
}

// leaveSpy records whether pop ran its Leave, once, with the host.
type leaveSpy struct {
	focusSaved
	left int
}

func (s *leaveSpy) Leave(h surfaceHost) {
	if h == nil {
		panic("Leave ran with no host")
	}
	s.left++
}

// pop runs a surface's Leave exactly once, and leaving the mode under a
// stack of surfaces runs each one, top first.
func TestPopRunsLeaveOnce(t *testing.T) {
	m := buildModel(t)
	spy := &leaveSpy{focusSaved: *newFocusSaved(0, m.spawnStyles())}
	m.push(spy)
	m.pop()
	m.pop()
	if spy.left != 1 {
		t.Fatalf("Leave ran %d times, want 1", spy.left)
	}
	m.push(spy)
	m.push(newFocusSaved(1, m.spawnStyles()))
	m.clearSurfaces()
	if spy.left != 2 || len(m.surfaces) != 0 {
		t.Fatalf("after clearSurfaces: Leave ran %d times, %d surfaces left", spy.left, len(m.surfaces))
	}
}
