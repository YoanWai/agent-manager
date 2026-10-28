package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/systheme"
	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// onboardingFeature is the first-run setup and what the root found for it:
// the agents on PATH and the OS light or dark scheme.
type onboardingFeature struct {
	uionboarding.Model
	scheme systheme.Scheme
	probed bool
	// ticking is a tick of the guide's animation on its way.
	ticking bool
}

// setupProbedMsg is the PATH and OS lookup the setup opened with.
type setupProbedMsg struct {
	installed map[string]bool
	scheme    systheme.Scheme
}

// openSetup shows the setup on the current settings. firstRun lets it check
// the agents it finds installed.
func (m *Model) openSetup(firstRun bool) tea.Cmd {
	m.startup.setup = onboardingFeature{Model: uionboarding.New(m.services.cfg.AgentToolNames(), m.setupChoices(), firstRun)}
	m.mode = modeOnboarding
	return m.setupProbeCmd()
}

// setupOnLaunch starts the lookup for a setup the launch opened, before
// the program could run a command.
func (m *Model) setupOnLaunch() tea.Cmd {
	if m.mode != modeOnboarding {
		return nil
	}
	return m.setupProbeCmd()
}

func (m *Model) setupChoices() uionboarding.Choices {
	cache := m.settings.cache
	flag := func(key string) bool { return uisettings.FlagOn(key, cache.value(key)) }
	return uionboarding.Choices{
		Hidden:      settingsTools(m.services.cfg).KnownHidden(cache.hidden),
		FullLayout:  flag(uisettings.SessionLayoutSetting),
		Comfortable: flag(uisettings.ListDensitySetting),
		Theme:       uitheme.Palettes[uitheme.Index(cache.value(uisettings.ThemeSetting))].Name,
		ThemeAuto:   flag(uisettings.ThemeAutoSetting),
	}
}

// setupProbeCmd looks each agent CLI up on PATH and asks the OS for its
// scheme, both of which shell out, off the update path.
func (m *Model) setupProbeCmd() tea.Cmd {
	commands := make(map[string]string)
	for _, name := range m.services.cfg.AgentToolNames() {
		commands[name] = m.services.cfg.Tools[name].Command
	}
	return func() tea.Msg {
		installed := make(map[string]bool, len(commands))
		for name, command := range commands {
			installed[name] = config.CheckInstalled(command) == nil
		}
		return setupProbedMsg{installed: installed, scheme: systheme.Detect()}
	}
}

// applySetupProbe takes the lookup into the open setup. A setup closed
// before it answered has no use for it.
func (m *Model) applySetupProbe(probe setupProbedMsg) tea.Cmd {
	if m.mode != modeOnboarding {
		return nil
	}
	setup := &m.startup.setup
	setup.SetInstalled(probe.installed)
	setup.scheme, setup.probed = probe.scheme, true
	return tea.Batch(m.syncSetupPalette(), m.keepSetupAnimating())
}

func (m *Model) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.applySetupOutcome(m.startup.setup.Key(msg))
}

func (m *Model) applySetupOutcome(out uionboarding.Outcome) (tea.Model, tea.Cmd) {
	var save tea.Cmd
	switch out.Action {
	case uionboarding.Save:
		save = m.saveSetup(out.Choices)
	case uionboarding.Close:
		m.mode = modeList
	}
	return m, tea.Batch(save, m.syncSetupPalette(), m.keepSetupAnimating())
}

// saveSetup writes the confirmed choices down the Settings save path, so
// a choice made here is stored, mirrored and read back exactly as one made
// in Settings.
func (m *Model) saveSetup(choices uionboarding.Choices) tea.Cmd {
	s := &m.settings
	s.dialog = s.stateFromCache(m.services.cfg)
	d := &s.dialog
	// The baseline is the stored theme, not the palette the theme step is
	// previewing on screen.
	d.ShowTheme(d.ManualTheme)
	d.MarkBaseline()
	d.CLIHidden = choices.Hidden
	d.ShowTools(settingsTools(m.services.cfg), s.cache.value(uisettings.DefaultToolSetting))
	d.FullLayout, d.ComfortableRows = choices.FullLayout, choices.Comfortable
	d.ThemeAuto, d.ManualTheme = choices.ThemeAuto, choices.Theme
	d.ShowTheme(m.setupPalette(choices))
	m.applySettingsPrefs()
	m.rebuildRows()
	return m.captureSettingsSave(true, false)
}

// setupPalette is the palette choices paint with. Following the OS keeps the
// palette on screen until the scheme lookup answers.
func (m *Model) setupPalette(choices uionboarding.Choices) string {
	if !choices.ThemeAuto {
		return choices.Theme
	}
	if !m.startup.setup.probed {
		return current().Name
	}
	return autoThemeName(choices.Theme, m.startup.setup.scheme)
}

// syncSetupPalette repaints in the palette the setup shows: the one under
// the cursor on the theme step, the saved one anywhere else and once the
// setup closes.
func (m *Model) syncSetupPalette() tea.Cmd {
	name := m.setupPalette(m.startup.setup.Shown())
	if name == current().Name {
		return nil
	}
	applyTheme(themes[themeIndex(name)])
	SyncTerminalColors()
	return m.syncPaneTheme()
}

// setupKeys are the first key of each action the guide names, as bound now.
func (m *Model) setupKeys() uionboarding.Keys {
	first := func(table keybind.Table, action string) string {
		keys := table.Binding(action).Keys()
		if len(keys) == 0 {
			return ""
		}
		return keys[0].Glyph()
	}
	list := m.services.listKeys
	enter, detach := first(list, keybind.Open), first(m.services.keys, keybind.Detach)
	if !m.enterFocuses() {
		enter = first(list, keybind.Attach)
	}
	keys := uionboarding.Keys{
		Prompt:     first(list, keybind.Prompt),
		Focus:      first(list, keybind.StepIn),
		Back:       first(list, keybind.StepOut),
		OtherFocus: enter,
		OtherBack:  detach,
		NewSession: first(list, keybind.NewSession),
		NewGroup:   first(list, keybind.NewGroup),
		Review:     first(list, keybind.Review),
		Settings:   first(list, keybind.Settings),
		Messages:   first(list, keybind.Messages),
		Help:       first(list, keybind.Help),
	}
	if !m.prefs.arrowStep || keys.Focus == "" || keys.Back == "" {
		keys.Focus, keys.Back, keys.OtherFocus, keys.OtherBack = enter, detach, "", ""
	}
	return keys
}

// setupMargin is the footer's inset when the setup's content is narrower.
const setupMargin = 4

// viewSetup paints the setup over the whole terminal, its keys on a footer
// under a rule. Every row is the card's body, so a click lands on the
// option it paints.
func (m *Model) viewSetup() string {
	width, height := m.layout.width, m.layout.height
	foot := paintInline(m.startup.setup.Hints(), max(width-2*setupMargin, 1), m.prefs.glyphMode)
	footRows := splitLines(foot.text)
	frame := m.startup.setup.View(uionboarding.ViewContext{
		Width:     width,
		Height:    max(height-len(footRows)-2, 1),
		GlyphMode: m.prefs.glyphMode,
		Theme:     current(),
		Wordmark:  strings.TrimPrefix(m.viewBanner()[0], strings.Repeat(" ", railGutter)),
		Keys:      m.setupKeys(),
		Status:    m.statusRow(),
		Preview:   m.setupPreview,
	})
	indent := strings.Repeat(" ", frame.Left)
	lines := make([]string, 0, height)
	for _, line := range frame.Lines {
		lines = append(lines, paint(line, width, backdropHex()))
	}
	lines = append(lines, paint(indent+hrule(width-2*frame.Left), width, backdropHex()))
	m.layout.card = cardRect{right: width, bottom: height, bodyWidth: width}
	m.layout.keyHits = nil
	m.placeKeys(foot.hits, frame.Left, len(lines))
	for _, row := range footRows {
		lines = append(lines, paint(indent+row, width, backdropHex()))
	}
	lines = append(lines, paint("", width, backdropHex()))
	return strings.Join(lines, "\n")
}
