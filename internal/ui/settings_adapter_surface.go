package ui

import (
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsRoot is the root as the Settings card reads it.
type settingsRoot struct{ *Model }

func (r settingsRoot) Context() uisettings.Context { return r.settingsContext() }

func (r settingsRoot) Apply(d *uisettings.State, result uisettings.Result) (tea.Cmd, bool) {
	return r.applySettingsResult(d, result)
}

func (r settingsRoot) Leave(d *uisettings.State) { r.leaveSettings(d) }

func (r settingsRoot) Paint(d *uisettings.State) string { return r.paintSettings(d) }

func newSettingsSurface(m *Model, state settingsState) *uisettings.Surface {
	return uisettings.NewSurface(settingsRoot{m}, state)
}

// columnClicker is the optional capability of a surface whose card acts on
// where in a line a click lands: the root hands it the column too. It
// bridges until surface.Click carries the column for every card.
type columnClicker interface {
	ClickAt(h surfaceHost, col, line int) tea.Cmd
}

var _ columnClicker = (*uisettings.Surface)(nil)
