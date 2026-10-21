package focus

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEncodeKey(t *testing.T) {
	cases := []struct {
		name     string
		msg      tea.KeyMsg
		wantRaw  string
		wantName string
		ok       bool
	}{
		{"runes", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")}, "hi", "", true},
		{"utf8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ש")}, "ש", "", true},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, " ", "", true},
		{"alt-rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, "\x1bx", "", true},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "", "Enter", true},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "", "C-c", true},
		{"tab-not-ctrl-i", tea.KeyMsg{Type: tea.KeyTab}, "", "Tab", true},
		{"alt-up", tea.KeyMsg{Type: tea.KeyUp, Alt: true}, "", "M-Up", true},
		{"pgup", tea.KeyMsg{Type: tea.KeyPgUp}, "", "PPage", true},
		{"unnamed", tea.KeyMsg{Type: tea.KeyCtrlBackslash}, "", "", false},
		// A bracketed paste never rides the raw-bytes path: its newlines
		// would reach the agent as bare Enter presses and submit the prompt.
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("do it\n"), Paste: true}, "", "", false},
	}
	for _, c := range cases {
		raw, name, ok := EncodeKey(c.msg)
		if ok != c.ok || string(raw) != c.wantRaw || name != c.wantName {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q, %v)", c.name, raw, name, ok, c.wantRaw, c.wantName, c.ok)
		}
	}
}
