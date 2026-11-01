package ui

import (
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func globalWindowStyle(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd(t, "show-options", "-gv", "window-style").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options window-style: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// The pane carries the theme's own backdrop, on both sides of the split, so
// an agent that follows it renders on the side the manager is drawing.
func TestAgentPaneTheme(t *testing.T) {
	t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	for _, tt := range []struct {
		theme string
		fgbg  string
	}{
		{"catppuccin latte", "0;15"},
		{"tokyo night", "15;0"},
	} {
		want := uitheme.Palettes[uitheme.Index(tt.theme)]
		applyTheme(want)
		got := paneTheme()
		if got.Background != want.Bg {
			t.Errorf("%s: pane background = %q, want %q", tt.theme, got.Background, want.Bg)
		}
		if got.Foreground != want.Text {
			t.Errorf("%s: pane foreground = %q, want %q", tt.theme, got.Foreground, want.Text)
		}
		if got.ColorFgBg != tt.fgbg {
			t.Errorf("%s: COLORFGBG = %q, want %q", tt.theme, got.ColorFgBg, tt.fgbg)
		}
	}
}

// Agents resolve their own palette against the pane background, so a theme
// switch has to reach the tmux server as well as the manager's own frame.
func TestThemeSwitchPushesPaneBackground(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	// Global options only stick while a server is up, and a server with no
	// sessions exits at once, so one session holds it open.
	if err := m.services.tmux.Create("panebg", "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { m.services.tmux.Kill("panebg") })
	t.Cleanup(func() { tmuxCmd(t, "set-option", "-gu", "window-style").Run() })

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme

	light := uitheme.Palettes[uitheme.Index("solarized light")]
	m.settingsCard().State().ThemeIndex = uitheme.Index("solarized light") - 1
	if cmd := m.cycleSetting(1); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
	if m.errBar.text != "" {
		t.Fatalf("pane theme push reported %q", m.errBar.text)
	}
	if got, want := globalWindowStyle(t), "fg="+light.Text+",bg="+light.Bg; got != want {
		t.Errorf("light theme pushed window-style %q, want its own backdrop %q", got, want)
	}

	m.settingsCard().State().ThemeIndex = uitheme.Index("nord") - 1
	if cmd := m.cycleSetting(1); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
	nord := uitheme.Palettes[uitheme.Index("nord")]
	if got, want := globalWindowStyle(t), "fg="+nord.Text+",bg="+nord.Bg; got != want {
		t.Errorf("dark theme pushed window-style %q, want %q", got, want)
	}
}

func TestResolveStartupThemeWithoutAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	if got := uitheme.ResolveStartup(m.services.store); got != "nord" {
		t.Fatalf("ResolveStartup = %q, want the stored theme", got)
	}
}

func TestThemeAutoPersistsWithoutClobberingManualTheme(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if m.settingsCard().State().ThemeAuto {
		t.Fatal("theme auto should default off")
	}
	m.settingsCard().State().Field = uisettings.FieldThemeAuto
	m.cycleSetting(1)
	if !m.settingsCard().State().ThemeAuto {
		t.Fatal("toggle should enable theme auto")
	}
	m.applyCmd(t, m.captureSettingsSave(m.settingsCard().State(), true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != "nord" {
		t.Fatalf("manual theme clobbered by auto: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "on" {
		t.Fatalf("theme_auto not persisted: %q", got)
	}
	if !uitheme.AutoEnabled(m.services.store) {
		t.Fatal("AutoEnabled should read the persisted toggle")
	}
}

func TestManualThemeCycleDisablesAuto(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "classic"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(uisettings.ThemeAutoSetting, "on"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if !m.settingsCard().State().ThemeAuto {
		t.Fatal("open should reflect the persisted auto toggle")
	}
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	if m.settingsCard().State().ThemeAuto {
		t.Fatal("stepping the theme by hand should turn auto off")
	}
	m.applyCmd(t, m.captureSettingsSave(m.settingsCard().State(), true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "off" {
		t.Fatalf("theme_auto should persist off after a manual step: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name {
		t.Fatalf("manual step not persisted: %q", got)
	}
}

func TestGlyphModesCoverEveryBuiltinTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		t.Run(string(mode), func(t *testing.T) {
			g := presentation.GlyphsFor(mode)
			seen := map[string]string{}
			states := []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Idle}
			for _, state := range states {
				mark := g.StatusGlyph(state)
				if other, ok := seen[mark]; ok {
					t.Errorf("%s and %s share %q", other, state, mark)
				}
				seen[mark] = state
				if mode == presentation.GlyphModeBasic && (len(mark) != 1 || mark[0] < '!' || mark[0] > '~') {
					t.Errorf("basic %s mark %q must be printable ASCII", state, mark)
				}
				if ansi.StringWidth(mark) != 1 {
					t.Errorf("%s mark %q must occupy one cell", state, mark)
				}
			}
			if g.StatusGlyph(status.Dead) != g.StatusGlyph(status.Errored) {
				t.Fatal("dead sessions lost their error mark")
			}
			marks := append([]string(nil), g.StartupFrames...)
			fields := reflect.ValueOf(g)
			for i := 0; i < fields.NumField(); i++ {
				if field := fields.Field(i); field.Kind() == reflect.String {
					marks = append(marks, field.String())
				}
			}
			for _, mark := range marks {
				if ansi.StringWidth(mark) != 1 {
					t.Errorf("mark %q must occupy one cell", mark)
				}
				if mode == presentation.GlyphModeBasic && (len(mark) != 1 || mark[0] < '!' || mark[0] > '~') {
					t.Errorf("basic mark %q must be printable ASCII", mark)
				}
			}
			for _, toolName := range append(slices.Sorted(maps.Keys(cfg.CLIs)), cfg.ToolNames()...) {
				m := &Model{services: services{cfg: cfg}, prefs: preferences{glyphMode: mode}}
				for _, state := range append(states, status.Dead) {
					sess := store.Session{Tool: toolName, Status: state}
					for phase := 0; phase < len(g.StartupFrames)*2; phase++ {
						m.startup.LoadPhase = phase
						want := g.StatusGlyph(state)
						if state == status.Starting {
							want = g.StartupFrames[phase%len(g.StartupFrames)]
						} else if cfg.IsTool(toolName) && state != status.Errored && state != status.Dead {
							want = g.ShellGlyph
						}
						if got := ansi.Strip(m.sessionGlyph(sess)); got != want {
							t.Errorf("%s %s phase %d: got %q, want %q", toolName, state, phase, got, want)
						}
					}
				}
			}
		})
	}
}

func TestGlyphModeKeepsModelsIndependentAndAdvancedAppearance(t *testing.T) {
	basic, advanced := &Model{prefs: preferences{glyphMode: presentation.GlyphModeBasic}}, &Model{prefs: preferences{glyphMode: presentation.GlyphModeAdvanced}}
	want := map[string]string{status.Working: "◐", status.Starting: "◌", status.Waiting: "◆", status.Finished: "●", status.Errored: "✕", status.Idle: "○"}
	for state, mark := range want {
		if got := advanced.glyphs().StatusGlyph(state); got != mark {
			t.Errorf("advanced %s = %q want %q", state, got, mark)
		}
		if basic.glyphs().StatusGlyph(state) == mark {
			t.Errorf("advanced choice leaked into basic model for %s", state)
		}
	}
	g := advanced.glyphs()
	if g.ReorderGrip != "⠿" || g.InboxGlyph != "✉" || g.BranchGlyph != "⑂" || g.GroupOpenGlyph != "▾" || g.GroupClosedGlyph != "▸" {
		t.Fatalf("advanced controls changed: %+v", g)
	}
	for _, m := range []*Model{basic, advanced} {
		m.services.keys = keybind.DefaultSession()
		m.services.listKeys = keybind.DefaultList()
		m.openHelp()
		m.layout.width, m.layout.height = 120, 100
		if !strings.Contains(ansi.Strip(viewHelpOf(m)), m.glyphs().StatusGlyph(status.Working)+" working") {
			t.Fatal("help and renderer disagree")
		}
		ring := ansi.Strip(strings.Join(uistartup.Ring(m.glyphs(), 40, 10, "loading", 0, uitheme.Current()), "\n"))
		for _, mark := range []string{m.glyphs().LoaderHead, m.glyphs().LoaderTail, m.glyphs().LoaderRest} {
			if !strings.Contains(ring, mark) {
				t.Errorf("loader missing %q: %s", mark, ring)
			}
		}
	}
}

func TestUnknownStoredSymbolsSettingSurfacesAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st := openTestStore(t, path)
	if err := st.SetSetting(uisettings.GlyphModeSetting, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	m := buildModelWithStorePath(t, path)
	if m.prefs.glyphMode != "" || !strings.Contains(m.errBar.text, "unknown symbols setting: unknown") {
		t.Fatalf("invalid stored preference: mode=%q error=%q", m.prefs.glyphMode, m.errBar.text)
	}
	if m.glyphs().ReorderGrip != presentation.GlyphsFor(presentation.DefaultGlyphMode).ReorderGrip {
		t.Fatal("an unknown symbols setting must paint the default marks")
	}
}

func TestPreviewKeepsTheAgentsOwnSymbolsInCompatibleMode(t *testing.T) {
	const text = "◐◌◆●○✕ ⠿ ✉ ⑂ ⠋ 界é"
	m := previewModel(status.Idle, text+"\n")
	m.prefs.glyphMode = presentation.GlyphModeBasic
	if got := previewText(m); !strings.Contains(got, text) {
		t.Fatalf("preview changed the CLI's symbols: %q", got)
	}
}

func TestPendingActionMarksMatchHelpInBothModes(t *testing.T) {
	for _, tc := range []struct {
		mode          presentation.GlyphMode
		archive, kill string
	}{{presentation.GlyphModeBasic, "A", "K"}, {presentation.GlyphModeAdvanced, "↓", "■"}} {
		m := shotModel()
		m.prefs.glyphMode = tc.mode
		for action, want := range map[string]string{store.AfterTurnArchive: tc.archive, store.AfterTurnKill: tc.kill} {
			if got := ansi.Strip(m.afterTurnBadge(action)); got != want {
				t.Fatalf("%s/%s badge=%q", tc.mode, action, got)
			}
		}
		found := false
		m.openHelp()
		m.layout.width, m.layout.height = 120, 100
		found = strings.Contains(ansi.Strip(viewHelpOf(m)), tc.archive+" / "+tc.kill)
		if !found {
			t.Fatalf("%s help has different lifecycle marks", tc.mode)
		}
	}
}
