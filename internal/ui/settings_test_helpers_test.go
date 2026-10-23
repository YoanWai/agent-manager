package ui

import (
	"sync"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// blockedSettingReader parks a store read until the test releases it, so a
// load that would block Update can be proven off it.
type blockedSettingReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	values  map[string]string
}

func (r *blockedSettingReader) get(key string) (string, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.values[key], nil
}

// settingsCard is the open Settings card; a test that reads it with none up
// is wrong, so it panics.
func (m *Model) settingsCard() *uisettings.Surface {
	s, ok := surfaceOf[*uisettings.Surface](m)
	if !ok {
		panic("the Settings card is not open")
	}
	return s
}

func (m *Model) settingsOpen() bool {
	_, ok := surfaceOf[*uisettings.Surface](m)
	return ok
}

// handleSettingsKey presses a key through the stack the way a terminal does.
func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) { return m.Update(msg) }

func (m *Model) viewSettings() string {
	return m.paintSettings(m.settingsCard().State())
}

// closeSettings is the save-and-close a key or a click on the button asks.
func (m *Model) closeSettings() (tea.Model, tea.Cmd) {
	m.Pop()
	return m, m.nextEffectCmd()
}
