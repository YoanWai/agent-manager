package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

// defaultSplitLayout reports whether review mode should open in split
// (side-by-side) layout. Split is the default; a stored "unified" choice
// opts out. A store error is surfaced but still yields the split default.
func (m *Model) defaultSplitLayout() bool {
	chosen, err := m.services.store.Setting(uisettings.DiffLayoutSetting)
	if err != nil {
		m.reportErr("reading diff layout setting: " + err.Error())
		return true
	}
	return chosen != "unified"
}

// storedTerminalBackground reads the background row. A store error is
// surfaced but still yields the painted default.
func (m *Model) storedTerminalBackground() bool {
	chosen, err := m.services.store.Setting(uisettings.BackgroundSetting)
	if err != nil {
		m.reportErr("reading background setting: " + err.Error())
	}
	return chosen == "terminal"
}

// enterFocuses reports which key opens a session where. Enter focuses the
// preview and A attaches full screen by default; a stored "attach" choice
// swaps the pair. Cached on the model because the footer reads it every
// frame.
func (m *Model) enterFocuses() bool {
	return m.prefs.focusOnEnter
}

// mirrorPreference copies one stored setting into the running manager; a
// key the frame never reads stays in the cache alone.
func (m *Model) mirrorPreference(key, value string) {
	switch key {
	case uisettings.GlyphModeSetting:
		m.prefs.glyphMode = presentation.GlyphMode(value)
	case uisettings.EditorSetting:
		m.services.editor = value
	default:
		m.mirrorFlag(key, uisettings.FlagOn(key, value))
	}
}

// mirrorFlag is the one place a two-way row meets its preferences field.
func (m *Model) mirrorFlag(key string, on bool) {
	switch key {
	case uisettings.FocusKeySetting:
		m.prefs.focusOnEnter = on
	case uisettings.ArrowStepSetting:
		m.prefs.arrowStep = on
	case uisettings.ListDensitySetting:
		m.prefs.comfortableRows = on
	case uisettings.SessionLayoutSetting:
		m.prefs.fullLayout = on
	case uisettings.HideHeaderSetting:
		m.prefs.hideHeader = on
	case uisettings.HideStatsSetting:
		m.prefs.hideStats = on
	case uisettings.MouseSetting:
		m.prefs.mouseDisabled = on
	case uisettings.BackgroundSetting:
		m.prefs.terminalBackground = on
	case uisettings.BaseFetchSetting:
		m.prefs.baseFetchOff = !on
	}
}

func (m *Model) applyCachedSettingsPrefs() {
	for _, key := range uisettings.Keys() {
		m.mirrorPreference(key, m.settings.cache.value(key))
	}
}
