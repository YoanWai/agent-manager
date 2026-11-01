package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	onboardingruntime "github.com/YoanWai/agent-manager/internal/ui/onboarding/runtime"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

type onboardingRoot struct{ *Model }

func (h onboardingRoot) Paint(s *uionboarding.Surface) string { return h.viewSetup(s) }

func (m *Model) setupSurface() (*uionboarding.Surface, bool) {
	return surfaceOf[*uionboarding.Surface](m)
}
func (m *Model) setupOpen() bool { _, ok := m.setupSurface(); return ok }

func (m *Model) openSetup(firstRun bool) tea.Cmd {
	m.push(uionboarding.NewSurface(onboardingRoot{m}, m.services.cfg.CLINames(), m.setupChoices(), firstRun))
	if firstRun {
		return nil
	}
	return m.setupProbeCmd()
}
func (m *Model) setupOnLaunch() tea.Cmd {
	if !m.setupOpen() {
		return nil
	}
	return m.setupProbeCmd()
}

func (m *Model) setupChoices() uionboarding.Choices {
	cache := m.settings.Cache
	flag := func(key string) bool { return uisettings.FlagOn(key, cache.Value(key)) }
	return uionboarding.Choices{
		Hidden:      uipersist.Tools(m.services.cfg).KnownHidden(cache.Hidden()),
		FullLayout:  flag(uisettings.SessionLayoutSetting),
		Comfortable: flag(uisettings.ListDensitySetting),
		Theme:       uitheme.Palettes[uitheme.Index(cache.Value(uisettings.ThemeSetting))].Name,
		ThemeAuto:   flag(uisettings.ThemeAutoSetting),
	}
}

func (m *Model) setupProbeCmd() tea.Cmd {
	setup, ok := m.setupSurface()
	if !ok {
		return nil
	}
	commands := make(map[string]string)
	for _, name := range m.services.cfg.CLINames() {
		commands[name] = m.services.cfg.CLIs[name].Command
	}
	m.enqueueEffect(onboardingruntime.ProbeRequest{Target: setup.ID(), Commands: commands}, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) applySetupProbe(request onboardingruntime.ProbeRequest, probe onboardingruntime.ProbeResult, err error) tea.Cmd {
	setup, ok := m.setupSurface()
	if !ok || setup.ID() != request.Target {
		return nil
	}
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	setup.SetProbe(probe.Installed, func(name string) string { return uitheme.Auto(name, probe.Scheme) })
	return tea.Batch(m.syncSetupPalette(), setup.KeepAnimating())
}

func (m *Model) routeSetupMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case openSetupMsg:
		if m.screenID() != msg.Screen || m.gens.foreground != msg.ForegroundGen {
			return routed(m, nil)
		}
		return routed(m, m.openSetup(false))
	case uionboarding.OutcomeMsg:
		setup, ok := m.setupSurface()
		if !ok || setup != msg.Surface {
			return routed(m, nil)
		}
		var commands []tea.Cmd
		for _, outcome := range setup.TakeOutcomes() {
			_, cmd := m.applySetupOutcome(outcome)
			commands = append(commands, cmd)
			if !m.setupOpen() {
				break
			}
		}
		return routed(m, tea.Batch(commands...))
	case uionboarding.TickMsg:
		setup, ok := m.setupSurface()
		if !ok || setup != msg.Surface {
			return routed(m, nil)
		}
		return routed(m, setup.Advance())
	}
	return nil, nil, false
}

type openSetupMsg struct {
	Screen        screenID
	ForegroundGen uint64
}

func (m *Model) applySetupOutcome(out uionboarding.Outcome) (tea.Model, tea.Cmd) {
	setup, ok := m.setupSurface()
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	switch out.Action {
	case uionboarding.Save:
		cmd = m.saveSetup(out.Choices)
	case uionboarding.Close:
		m.Pop()
	case uionboarding.StartImport:
		m.Pop()
		cmd = m.startSetupImport(out.Import)
	}
	palette := m.showSetupPalette(setup.Palette(setup.Shown(), uitheme.Current().Name))
	if !m.setupOpen() {
		return m, tea.Batch(cmd, palette)
	}
	return m, tea.Batch(cmd, palette, setup.KeepAnimating())
}

// saveSetup writes the confirmed choices down the Settings save path, so
// a choice made here is stored, mirrored and read back exactly as one made
// in Settings.
func (m *Model) saveSetup(choices uionboarding.Choices) tea.Cmd {
	d := m.settings.FromCache(m.services.cfg, uitheme.Current().Name)
	d.ShowTheme(d.ManualTheme)
	d.MarkBaseline()
	d.CLIHidden = choices.Hidden
	d.ShowTools(uipersist.Tools(m.services.cfg), m.settings.Cache.Value(uisettings.DefaultToolSetting))
	d.FullLayout, d.ComfortableRows = choices.FullLayout, choices.Comfortable
	d.ThemeAuto, d.ManualTheme = choices.ThemeAuto, choices.Theme
	setup, _ := m.setupSurface()
	d.ShowTheme(setup.Palette(choices, uitheme.Current().Name))
	m.applySettingsPrefs(&d)
	m.rebuildRows()
	return m.captureSettingsSave(&d, true, false)
}

func (m *Model) syncSetupPalette() tea.Cmd {
	setup, ok := m.setupSurface()
	if !ok {
		return nil
	}
	return m.showSetupPalette(setup.Palette(setup.Shown(), uitheme.Current().Name))
}
func (m *Model) showSetupPalette(name string) tea.Cmd {
	if name == uitheme.Current().Name {
		return nil
	}
	applyTheme(uitheme.Palettes[uitheme.Index(name)])
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
func (m *Model) viewSetup(setup *uionboarding.Surface) string {
	if setup.Leaving() {
		return m.confirmCard("Leave setup", "Leave the setup?",
			"What you set so far stays. The first-run setup row in Settings brings it back.", false, "leave")
	}
	width, height := m.layout.width, m.layout.height
	foot := uiframe.PaintInline(setup.Hints(), max(width-2*setupMargin, 1), uiframe.LiveCap(m.prefs.glyphMode))
	footRows := uitheme.SplitLines(foot.Text)
	frame := setup.Model.View(uionboarding.ViewContext{
		Width:     width,
		Height:    max(height-len(footRows)-2, 1),
		GlyphMode: m.prefs.glyphMode,
		Theme:     uitheme.Current(),
		Wordmark:  strings.TrimPrefix(m.viewBanner()[0], strings.Repeat(" ", uiframe.RailGutter)),
		Keys:      m.setupKeys(),
		Status:    m.statusRow(),
		Preview:   m.setupPreview,
	})
	indent := strings.Repeat(" ", frame.Left)
	lines := make([]string, 0, height)
	for _, line := range frame.Lines {
		lines = append(lines, uitheme.Paint(line, width, uitheme.BackdropHex()))
	}
	lines = append(lines, uitheme.Paint(indent+uitheme.Hrule(width-2*frame.Left), width, uitheme.BackdropHex()))
	m.layout.card = cardRect{right: width, bottom: height, bodyWidth: width}
	m.layout.keyHits = nil
	m.placeKeys(foot.Hits, frame.Left, len(lines))
	for _, row := range footRows {
		lines = append(lines, uitheme.Paint(indent+row, width, uitheme.BackdropHex()))
	}
	lines = append(lines, uitheme.Paint("", width, uitheme.BackdropHex()))
	return strings.Join(lines, "\n")
}
