package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func linkModel(rows []string, width int) *Model {
	m := shotModel()
	m.mode = modeFocus
	m.preview = strings.Join(rows, "\n") + "\n"
	m.pane.box = paneBox{x: 0, y: 0, width: width, height: len(rows), ok: true}
	return m
}

func TestLinkAtFindsTheLinkUnderTheClick(t *testing.T) {
	m := linkModel([]string{
		"read https://example.com/docs, then reply",
		"no link on this row",
	}, 80)
	if got := m.linkAt(0, 8); got != "https://example.com/docs" {
		t.Fatalf("click inside the link = %q", got)
	}
	if got := m.linkAt(0, 2); got != "" {
		t.Fatalf("click before the link = %q, want none", got)
	}
	if got := m.linkAt(1, 4); got != "" {
		t.Fatalf("click on a linkless row = %q, want none", got)
	}
	if got := m.linkAt(7, 0); got != "" {
		t.Fatalf("click past the pane = %q, want none", got)
	}
}

func TestLinkAtTrimsQuotingPunctuation(t *testing.T) {
	m := linkModel([]string{
		`see (https://example.com/a).`,
		`wiki https://en.wikipedia.org/wiki/Go_(language) rocks`,
	}, 80)
	if got := m.linkAt(0, 10); got != "https://example.com/a" {
		t.Fatalf("bracketed link = %q", got)
	}
	if got := m.linkAt(1, 12); got != "https://en.wikipedia.org/wiki/Go_(language)" {
		t.Fatalf("link owning its parens = %q", got)
	}
}

func TestLinkAtRefusesNonWebSchemes(t *testing.T) {
	m := linkModel([]string{"open file:///etc/passwd or ftp://host/x now"}, 80)
	if got := m.linkAt(0, 8); got != "" {
		t.Fatalf("file scheme = %q, want none", got)
	}
	if got := m.linkAt(0, 30); got != "" {
		t.Fatalf("ftp scheme = %q, want none", got)
	}
}

func TestLinkAtJoinsAWrappedLink(t *testing.T) {
	width := 20
	m := linkModel([]string{
		"https://example.com/", // runs to the pane's edge
		"long/path more text",
	}, width)
	if got := m.linkAt(0, 5); got != "https://example.com/long/path" {
		t.Fatalf("wrapped link = %q", got)
	}
	// A next row that indents is a new block, not a continuation.
	m = linkModel([]string{
		"https://example.com/",
		"  indented follow-up",
	}, width)
	if got := m.linkAt(0, 5); got != "https://example.com/" {
		t.Fatalf("indented next row still joined = %q", got)
	}
	// A link ending short of the edge never joins.
	m = linkModel([]string{
		"https://example.com/a",
		"unrelated",
	}, 80)
	if got := m.linkAt(0, 5); got != "https://example.com/a" {
		t.Fatalf("unwrapped link joined = %q", got)
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
		m := linkModel([]string{"read https://example.com/docs now"}, 80)
		m.pane.mouse = tracking
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
		if m.sel.active {
			t.Fatalf("tracking=%v: a link click left a selection standing", tracking)
		}
	}

	// A click away from any link keeps its old meaning.
	opened = ""
	m := linkModel([]string{"read https://example.com/docs now"}, 80)
	m.pane.mouse = false
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	if opened != "" {
		t.Fatalf("a linkless click opened %q", opened)
	}
}
