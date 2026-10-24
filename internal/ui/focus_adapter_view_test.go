package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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

// A session open full screen owns the whole body, so the frame paints no
// rail line and leaves no hit behind for a click to resolve against.
func TestFullFocusFrameRecordsNoRailHits(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)
	if len(m.layout.displayedRail.Hits()) == 0 {
		t.Fatal("test setup: the list frame should record the rail's hits")
	}

	m.prefs.fullLayout = true
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.fullFocus() {
		t.Fatalf("test setup: focus alpha full screen, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	preparedView(m)
	if len(m.layout.displayedRail.Hits()) != 0 {
		t.Fatalf("full focus paints no rail, got %d hits", len(m.layout.displayedRail.Hits()))
	}
}

// A lone click on a link opens it in both kinds of pane: held back and
// released in a mouse-tracking pane, resolved at release in a plain one.
func TestFocusClickOpensTheLink(t *testing.T) {
	opened := ""
	prev := openURL
	openURL = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openURL = prev })

	press := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 0}
	release := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 8, Y: 0}

	for _, tracking := range []bool{true, false} {
		opened = ""
		m, id := focusedShotPane(t, 80, 0, 0, "read https://example.com/docs now")
		updateFocusPane(m, id, func(update *uifocus.PaneUpdate) { update.Mouse = tracking })
		m.handleFocusMouse(press)
		_, cmd := m.handleFocusMouse(release)
		if cmd == nil {
			t.Fatalf("tracking=%v: click on a link returned no command", tracking)
		}
		if msg := cmd(); msg != nil {
			t.Fatalf("tracking=%v: opener errored: %v", tracking, msg)
		}
		if opened != "https://example.com/docs" {
			t.Fatalf("tracking=%v: opened %q", tracking, opened)
		}
	}

	// A click away from any link keeps its old meaning.
	opened = ""
	m, _ := focusedShotPane(t, 80, 0, 0, "read https://example.com/docs now")
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	_, cmd := m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	if cmd != nil {
		t.Fatal("a linkless click returned a command")
	}
	if opened != "" {
		t.Fatalf("a linkless click opened %q", opened)
	}
}

// The saved prompt key opens the list over a focused session, a pick is
// pasted into the agent's prompt unsent, and the leave key still leaves.
func TestFocusSavedPromptPastesThePick(t *testing.T) {
	m := buildModel(t)
	savePrompt(t, m, "Role", "line one\nline two")
	createSession(t, m, "paster", t.TempDir(), "")
	m.selectSessionRow(t, "paster")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("enter did not focus, screen = %v", m.screenID())
	}
	var pasted []string
	restore := pasteFocused
	pasteFocused = func(_ *tmux.Driver, _, text string) error {
		pasted = append(pasted, text)
		return nil
	}
	t.Cleanup(func() { pasteFocused = restore })

	_, load := m.handleKey(tea.KeyMsg{Type: tea.KeyF4})
	m.runBatch(t, load)
	m.prepareFrame()
	if !strings.Contains(ansi.Strip(m.View()), "Role") || !strings.Contains(ansi.Strip(m.viewFooter()), "paste") {
		t.Fatal("f4 did not show the list")
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runBatch(t, cmd)
	m.drainEffects(t)
	if len(pasted) != 1 || pasted[0] != "line one\nline two" || m.savedListOpen() || !m.focused() {
		t.Fatalf("pasted %q, list open %v, mode %v", pasted, m.savedListOpen(), m.screenID())
	}

	_, load = m.handleKey(tea.KeyMsg{Type: tea.KeyF4})
	m.runBatch(t, load)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !m.onList() || m.savedListOpen() {
		t.Fatalf("the leave key with the list up left mode %v, open %v", m.screenID(), m.savedListOpen())
	}
}
