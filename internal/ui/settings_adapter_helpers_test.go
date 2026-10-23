package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// cycleSetting steps the focused row as ← or → would.
func (m *Model) cycleSetting(step int) tea.Cmd {
	return m.runSettingsResult(m.settingsCard().State(), m.settingsCard().State().Cycle(step))
}

// openSettingsCLIPicker opens the CLI picker from its row.
func (m *Model) openSettingsCLIPicker(t *testing.T) {
	t.Helper()
	m.settingsCard().State().Field = uisettings.FieldCLIs
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().CLIPicker {
		t.Fatal("the CLIs row did not open the picker")
	}
}

// storedFlag reads a two-way row from the store, as the startup mirror does.
func storedFlag(t *testing.T, m *Model, key string) bool {
	t.Helper()
	value, err := m.services.store.Setting(key)
	if err != nil {
		t.Fatal(err)
	}
	return uisettings.FlagOn(key, value)
}
