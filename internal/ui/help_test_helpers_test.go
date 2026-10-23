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
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		prefs: preferences{
			arrowStep: true,
		},
	}
	m.openHelp()
	return m
}

// helpSurfaceOf is the key map on the stack, which a test reads state from.
func helpSurfaceOf(m *Model) *uihelp.Surface {
	s, ok := surfaceOf[*uihelp.Surface](m)
	if !ok {
		panic("the key map is not open")
	}
	return s
}

func helpLayoutOf(m *Model) uihelp.Card { return helpSurfaceOf(m).Card(m) }

func viewHelpOf(m *Model) string { return helpSurfaceOf(m).View(m, "") }

func helpOpen(m *Model) bool {
	_, ok := surfaceOf[*uihelp.Surface](m)
	return ok
}
