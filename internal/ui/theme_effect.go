package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// syncPaneTheme hands the tmux server the colors agent panes render on,
// so an agent that auto-detects its palette resolves to the same side the
// manager is drawing. Sessions that are already running keep whatever they
// resolved at startup; the theme reaches them on their next launch. The
// chosen theme is recorded synchronously so a session created right after
// still opens on it; the push to a running server shells out to tmux, so it
// runs as a command off the update path and surfaces a failure through errMsg.
func (m *Model) syncPaneTheme() tea.Cmd {
	m.services.tmux.PublishPaneTheme(agentPaneTheme())
	return func() tea.Msg {
		if err := m.services.tmux.PushPaneTheme(); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// agentPaneTheme is the backdrop and text color an agent pane sits on: the
// theme's own, the same colors the terminal is set to and the capture is
// drawn over. Agents that auto-detect follow it, so a light theme hosts
// light agents.
func agentPaneTheme() tmux.PaneTheme {
	fgbg := "15;0"
	if uitheme.Current().LightBackdrop() {
		fgbg = "0;15"
	}
	return tmux.PaneTheme{Foreground: uitheme.Current().Text, Background: uitheme.Current().Bg, ColorFgBg: fgbg}
}

// storedTheme reads the persisted theme name. A read failure falls back to
// the default theme: the UI still paints, just not in the chosen palette.
func storedTheme(st *store.Store) string {
	name, err := st.Setting(uisettings.ThemeSetting)
	if err != nil {
		return ""
	}
	return name
}

func themeAutoEnabled(st *store.Store) bool {
	value, err := st.Setting(uisettings.ThemeAutoSetting)
	return err == nil && value == "on"
}

// resolveStartupTheme picks the boot theme: the stored choice, unless
// auto-detect is on and the environment's scheme disagrees with that
// choice's polarity — then its counterpart, or the default theme of the
// detected side, takes over, and an undetectable scheme changes nothing.
func resolveStartupTheme(st *store.Store) string {
	stored := storedTheme(st)
	if !themeAutoEnabled(st) {
		return stored
	}
	return autoThemeName(stored, systheme.Detect())
}
