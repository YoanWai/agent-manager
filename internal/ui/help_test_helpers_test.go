package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func namedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func helpModel() *Model {
	return &Model{
		width:  120,
		height: 30,
		mode:   modeHelp,
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		prefs: preferences{
			arrowStep: true,
		},
	}
}

func featureHelpContext(width, height int) helpContext {
	return helpContext{
		sessionKeys: keybind.DefaultSession(),
		listKeys:    keybind.DefaultList(),
		arrowStep:   true,
		width:       width,
		height:      height,
	}
}
