package ui

import tea "github.com/charmbracelet/bubbletea"

// Host methods several feature types declare. Each feature's own host
// interface names the subset it uses.

func (m *Model) size() (width, height int) { return m.layout.width, m.layout.height }

func (m *Model) currentMode() mode { return m.mode }

// setMode switches the root's active mode; a dialog closes through it.
func (m *Model) setMode(next mode) { m.mode = next }

// quit is requestQuit for feature hosts, which get the command but never the model.
func (m *Model) quit() tea.Cmd {
	_, cmd := m.requestQuit()
	return cmd
}
