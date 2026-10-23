package ui

import tea "github.com/charmbracelet/bubbletea"

// settingsCard is the open Settings card; a test that reads it with none up
// is wrong, so it panics.
func (m *Model) settingsCard() *settingsSurface {
	s, ok := surfaceOf[*settingsSurface](m)
	if !ok {
		panic("the Settings card is not open")
	}
	return s
}

func (m *Model) settingsOpen() bool {
	_, ok := surfaceOf[*settingsSurface](m)
	return ok
}

// handleSettingsKey presses a key through the stack the way a terminal does.
func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) { return m.Update(msg) }

func (m *Model) viewSettings() string {
	return m.paintSettings(&m.settingsCard().state)
}

// closeSettings is the save-and-close a key or a click on the button asks.
func (m *Model) closeSettings() (tea.Model, tea.Cmd) {
	m.pop()
	return m, m.nextEffectCmd()
}
