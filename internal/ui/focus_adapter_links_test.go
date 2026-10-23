package ui

import (
	"testing"

	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
)

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
