package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFocusKeyCommand(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
		ok   bool
	}{
		{"runes", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")}, "send-keys -t am_x -H 68 69", true},
		{"utf8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ש")}, "send-keys -t am_x -H d7 a9", true},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, "send-keys -t am_x -H 20", true},
		{"alt-rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, "send-keys -t am_x -H 1b 78", true},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "send-keys -t am_x Enter", true},
		{"escape", tea.KeyMsg{Type: tea.KeyEsc}, "send-keys -t am_x Escape", true},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "send-keys -t am_x C-c", true},
		{"tab-not-ctrl-i", tea.KeyMsg{Type: tea.KeyTab}, "send-keys -t am_x Tab", true},
		{"enter-not-ctrl-m", tea.KeyMsg{Type: tea.KeyEnter}, "send-keys -t am_x Enter", true},
		{"shift-tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "send-keys -t am_x BTab", true},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "send-keys -t am_x Up", true},
		{"alt-up", tea.KeyMsg{Type: tea.KeyUp, Alt: true}, "send-keys -t am_x M-Up", true},
		{"pgup", tea.KeyMsg{Type: tea.KeyPgUp}, "send-keys -t am_x PPage", true},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "send-keys -t am_x BSpace", true},
	}
	for _, c := range cases {
		got, ok := focusKeyCommand("am_x", c.msg)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
