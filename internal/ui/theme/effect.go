package theme

import (
	"github.com/YoanWai/agent-manager/internal/systheme"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// The settings rows this package reads.
const (
	// ThemeSetting is the chosen palette's name.
	ThemeSetting = "theme"
	// ThemeAutoSetting follows the OS color scheme when it is "on".
	ThemeAutoSetting = "theme_auto"
)

// Setting is a store row this package reads.
type Setting interface {
	Setting(key string) (string, error)
}

// PanePublisher carries the colors agent panes render on to the tmux server.
type PanePublisher interface {
	PushPaneTheme() error
}

// Services is what the theme effect lane captures for one job.
type Services struct{ Tmux PanePublisher }

// PaneRequest pushes the pane colors the manager drew to a running tmux
// server. Sessions that are already up keep whatever they resolved at
// startup; the theme reaches them on their next launch. Publishing itself
// happened at dispatch, so a session created right after still opens on the
// chosen theme.
type PaneRequest struct{}

func (PaneRequest) EffectName() string { return "pane theme" }

// Run pushes the published pane colors, which shells out to tmux.
func (r PaneRequest) Run(s Services) (uieffects.Result, error) { return nil, s.Tmux.PushPaneTheme() }

// StoredName reads the persisted theme name. A read failure falls back to
// the default theme: the UI still paints, just not in the chosen palette.
func StoredName(st Setting) string {
	name, err := st.Setting(ThemeSetting)
	if err != nil {
		return ""
	}
	return name
}

// AutoEnabled reports whether the theme follows the OS color scheme.
func AutoEnabled(st Setting) bool {
	value, err := st.Setting(ThemeAutoSetting)
	return err == nil && value == "on"
}

// Auto resolves auto-detect against the detected scheme; an undetectable
// scheme keeps the stored choice.
func Auto(stored string, scheme systheme.Scheme) string {
	if scheme == systheme.SchemeUnknown {
		return stored
	}
	return AutoName(stored, scheme == systheme.SchemeLight)
}

// ResolveStartup picks the boot theme: the stored choice, unless auto-detect
// is on and the environment's scheme disagrees with that choice's polarity —
// then its counterpart, or the default theme of the detected side, takes
// over, and an undetectable scheme changes nothing.
func ResolveStartup(st Setting) string {
	stored := StoredName(st)
	if !AutoEnabled(st) {
		return stored
	}
	return Auto(stored, systheme.Detect())
}
