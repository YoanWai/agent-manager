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
	if m.mode != modeFocus {
		t.Fatalf("enter did not focus, mode = %v", m.mode)
	}
	list := spawn.OpenSavedList(nil, true, false)
	list.Load([]spawn.Saved{{Name: "Role", Text: "be brief"}}, nil)
	m.pushSurface(&focusSaved{list: list})
	return m
}

// The surface on top owns the keyboard: the manager reads keys there, even
// over a focused session whose pane would otherwise take them.
func TestTopSurfaceOwnsTheKeyboard(t *testing.T) {
	m := focusWithSavedList(t)
	if m.agentOwnsKeyboard() {
		t.Fatal("the agent owns the keyboard under an open list")
	}
	m.popSurface()
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
	if m.savedListOpen() || m.mode != modeFocus {
		t.Fatalf("esc: list open=%t mode=%v", m.savedListOpen(), m.mode)
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
	if m.mode != modeList || len(m.surfaces) != 0 {
		t.Fatalf("mode=%v surfaces=%d", m.mode, len(m.surfaces))
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
