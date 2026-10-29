package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"reflect"
)

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

// autoThemeName resolves auto-detect against the detected scheme; an
// undetectable scheme keeps the stored choice.
func autoThemeName(stored string, scheme systheme.Scheme) string {
	if scheme == systheme.SchemeUnknown {
		return stored
	}
	return uitheme.AutoName(stored, scheme == systheme.SchemeLight)
}

// colorSchemeMsg is a mode 2031 report: the terminal's light or dark
// scheme, sent once when reporting is enabled and again on every flip.
type colorSchemeMsg struct{ light bool }

// EnableColorSchemeReports asks the terminal to report its scheme as it
// changes, so theme follows OS holds past startup. Terminals and tmux
// releases without mode 2031 ignore it and keep the startup detection.
func EnableColorSchemeReports() error {
	_, err := os.Stdout.WriteString(ansi.SetModeLightDark)
	return err
}

func DisableColorSchemeReports() error {
	_, err := os.Stdout.WriteString(ansi.ResetModeLightDark)
	return err
}

// ColorSchemeFilter turns mode 2031 reports into colorSchemeMsg. Bubble Tea
// v1 has no message for them and delivers each as its unexported unknown
// CSI message, a byte slice holding the raw sequence.
func ColorSchemeFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	raw := reflect.ValueOf(msg)
	if raw.Kind() != reflect.Slice || raw.Type().Elem().Kind() != reflect.Uint8 {
		return msg
	}
	switch string(raw.Bytes()) {
	case ansi.LightDarkReport(true):
		return colorSchemeMsg{light: false}
	case ansi.LightDarkReport(false):
		return colorSchemeMsg{light: true}
	}
	return msg
}

// followColorScheme repaints for a reported scheme while theme follows OS.
// An open Settings dialog decides with its own, possibly unsaved, toggle
// and keeps showing the theme in use; the saved choice is never written.
func (m *Model) followColorScheme(msg colorSchemeMsg) tea.Cmd {
	var name string
	if m.mode == modeSettings {
		d := &m.settings.dialog
		if !d.ThemeAuto {
			return nil
		}
		name = uitheme.AutoName(d.ManualTheme, msg.light)
		d.ShowTheme(name)
	} else {
		if m.settings.cache.value(uisettings.ThemeAutoSetting) != "on" {
			return nil
		}
		name = uitheme.AutoName(m.settings.cache.value(uisettings.ThemeSetting), msg.light)
	}
	if name == current().Name {
		return nil
	}
	applyTheme(themes[themeIndex(name)])
	SyncTerminalColors()
	return m.syncPaneTheme()
}

type preferences struct {
	glyphMode presentation.GlyphMode
	// focusOnEnter mirrors the persisted focus-key setting; the footer
	// reads it every frame, so it lives here instead of the store.
	focusOnEnter bool
	// arrowStep mirrors the persisted ←→ step-in/step-out setting, read
	// on every keypress.
	arrowStep bool
	// comfortableRows mirrors the persisted list density: entries paint
	// their meta on a second line instead of alongside the name. Every
	// rail frame reads it, so it lives here instead of the store.
	comfortableRows bool
	// fullLayout mirrors the persisted sessions layout: the rail owns the
	// whole width, with no preview column beside it. Every list frame
	// reads it, so it lives here instead of the store.
	fullLayout bool
	// Header and stats visibility stay cached because rendering and sizing
	// read them every frame.
	hideHeader bool
	hideStats  bool
	// mouseDisabled mirrors the persisted mouse-reporting setting: true gives
	// the rail and content column back to the terminal's own click-drag text
	// selection. Read on every Update via syncMouseCapture. Named for its off
	// polarity, like hideHeader/hideStats, so a bare Model{} in a test still
	// defaults to mouse reporting on.
	mouseDisabled bool
	// terminalBackground leaves the backdrop's cells on the terminal's own
	// colors, for translucent windows. Off polarity, so a bare Model{}
	// paints the backdrop like the default does.
	terminalBackground bool
	// baseFetchOff mirrors the persisted fetch-on-spawn setting, read on
	// every Update while a worktree spawn is being set up.
	baseFetchOff bool
}
