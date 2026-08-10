package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
)

// caretModel is a focused model whose pane mirror is posed by hand: the
// captured rows, and the caret cell tmux reported over them.
func caretModel(t *testing.T, cursor paneCursor, rows ...string) *Model {
	t.Helper()
	engine, err := status.NewEngine(config.Config{Tools: map[string]config.Tool{
		"claude":    {ActivityCutoff: `(?m)^❯`},
		"gemini":    {ActivityCutoff: `(?m)^\s*[>!*] `},
		"unmarked":  {},
		"wide-mark": {ActivityCutoff: `(?m)^→`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	m := &Model{engine: engine, mode: modeFocus}
	m.preview = strings.Join(rows, "\n") + "\n"
	m.pane.forID = "s1"
	m.pane.cursor = cursor
	return m
}

// Left is only free to mean "back to the list" where the agent would do
// nothing with it: at the head of its prompt, with the marker alone to
// the caret's left.
func TestCaretAtInputStart(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		cursor paneCursor
		rows   []string
		want   bool
	}{
		// tmux trims a row's trailing blanks, so an empty prompt is the
		// marker alone with the caret out on padding the row lacks.
		{"empty prompt", "claude", paneCursor{x: 2, y: 1, ok: true}, []string{"output", "❯"}, true},
		// Claude pads its marker with a non-breaking space, so the cell
		// between marker and caret is blank without being an ASCII space.
		{"nbsp padded prompt", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"❯\u00a0"}, true},
		{"nbsp padded with input", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"❯\u00a0write a test"}, true},
		{"nbsp padded mid-input", "claude", paneCursor{x: 5, y: 0, ok: true}, []string{"❯\u00a0write a test"}, false},
		{"caret before typed text", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"❯ hi"}, true},
		{"caret after typed text", "claude", paneCursor{x: 4, y: 0, ok: true}, []string{"❯ hi"}, false},
		{"caret one in", "claude", paneCursor{x: 3, y: 0, ok: true}, []string{"❯ hi"}, false},
		{"caret on the marker", "claude", paneCursor{x: 0, y: 0, ok: true}, []string{"❯ hi"}, false},
		// A wrapped prompt's continuation rows carry no marker: Left there
		// reaches the end of the row above and belongs to the agent.
		{"wrapped continuation", "claude", paneCursor{x: 2, y: 1, ok: true}, []string{"❯ a long", "  wrapped"}, false},
		{"plain output row", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"some output"}, false},
		// The marker has to open the row: one quoted mid-line is not a prompt.
		{"quoted marker", "claude", paneCursor{x: 8, y: 0, ok: true}, []string{"we use ❯ here"}, false},
		{"indented marker", "gemini", paneCursor{x: 4, y: 0, ok: true}, []string{"  > "}, true},
		{"indented marker mid-input", "gemini", paneCursor{x: 6, y: 0, ok: true}, []string{"  > hi"}, false},
		{"tool without a marker", "unmarked", paneCursor{x: 2, y: 0, ok: true}, []string{"❯"}, false},
		{"unknown tool", "nosuch", paneCursor{x: 2, y: 0, ok: true}, []string{"❯"}, false},
		{"no cursor report", "claude", paneCursor{x: 2, y: 1}, []string{"output", "❯"}, false},
		{"cursor row past the capture", "claude", paneCursor{x: 2, y: 9, ok: true}, []string{"❯"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := caretModel(t, c.cursor, c.rows...)
			if got := m.caretAtInputStart("s1", c.tool); got != c.want {
				t.Fatalf("caretAtInputStart = %v, want %v", got, c.want)
			}
		})
	}
}

// A double-width marker is measured in cells, not runes, so the caret's
// column lines up with the one tmux reported.
func TestCaretAtInputStartMeasuresMarkerInCells(t *testing.T) {
	m := caretModel(t, paneCursor{x: 2, y: 0, ok: true}, "→ hi")
	if !m.caretAtInputStart("s1", "wide-mark") {
		t.Fatal("caret at the head of a wide-marker prompt was not recognised")
	}
}

// The mirror belongs to whichever session pushed it, and a scrolled-back
// pane's rows no longer line up with the live caret: neither can decide.
func TestCaretAtInputStartNeedsCurrentPane(t *testing.T) {
	m := caretModel(t, paneCursor{x: 2, y: 0, ok: true}, "❯")
	if m.caretAtInputStart("other", "claude") {
		t.Fatal("another session's pane mirror decided the caret")
	}
	m.focusScroll = 3
	if m.caretAtInputStart("s1", "claude") {
		t.Fatal("a scrolled-back pane decided the caret")
	}
}

// Left leaves focus at the head of the prompt and reaches the agent
// anywhere else, so a typed prompt keeps its caret movement.
func TestFocusLeftUnfocusesAtPromptHead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "leftie", t.TempDir(), "")
	m.selectSessionRow(t, "leftie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "claude-hooked"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 4, y: 0, ok: true}
	m.preview = "❯ hi\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left inside a typed prompt left focus, mode = %v", m.mode)
	}
	if m.errBar.text != "" {
		t.Fatalf("forwarding left set err: %q", m.errBar.text)
	}

	m.pane.cursor = paneCursor{x: 2, y: 0, ok: true}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left at the prompt head did not unfocus, mode = %v", m.mode)
	}
}

// Right steps into the row under the cursor: a session is focused, and a
// collapsed group opens without the toggle closing an open one.
func TestRightStepsIntoTheRow(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("grouped", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "stepin", t.TempDir(), "grouped")
	m.selectGroupRow(t, "grouped")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if !m.collapsed["grouped"] {
		t.Fatal("left did not close the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.collapsed["grouped"] {
		t.Fatal("right did not open the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.collapsed["grouped"] {
		t.Fatal("a second right closed the group it had opened")
	}

	m.selectSessionRow(t, "stepin")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("right did not focus the session, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

// Alt+Left is a word jump inside the prompt, so it stays the agent's.
func TestFocusAltLeftStaysWithTheAgent(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "altleft", t.TempDir(), "")
	m.selectSessionRow(t, "altleft")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "claude-hooked"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 2, y: 0, ok: true}
	m.preview = "❯ hi\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("alt+left left focus, mode = %v", m.mode)
	}
}
