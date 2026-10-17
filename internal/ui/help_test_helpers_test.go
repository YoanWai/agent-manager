package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func namedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func helpModel() *Model {
	return &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		mode: modeHelp,
		help: helpFeature{state: uihelp.New(uihelp.Global)},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		prefs: preferences{
			arrowStep: true,
		},
	}
}
