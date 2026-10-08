package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
)

// storedTheme reads the persisted theme name. A read failure falls back to
// the default theme: the UI still paints, just not in the chosen palette.
func storedTheme(st *store.Store) string {
	name, err := st.Setting(themeSetting)
	if err != nil {
		return ""
	}
	return name
}

func themeAutoEnabled(st *store.Store) bool {
	value, err := st.Setting(themeAutoSetting)
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

// autoThemeName keeps the stored theme whenever it already sits on the
// detected side, and otherwise prefers its counterpart, so auto-detect
// corrects polarity without discarding the family the user picked.
func autoThemeName(stored string, scheme systheme.Scheme) string {
	if scheme == systheme.SchemeUnknown {
		return stored
	}
	wantLight := scheme == systheme.SchemeLight
	theme := themes[themeIndex(stored)]
	if theme.lightBackdrop() == wantLight {
		return stored
	}
	if theme.Counterpart != "" {
		return theme.Counterpart
	}
	if wantLight {
		return "solarized light"
	}
	return "classic"
}

type preferences struct {
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
