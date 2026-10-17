package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/charmbracelet/x/ansi"
)

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
			for _, mark := range append([]string{g.ReorderGrip, g.InboxGlyph, g.BranchGlyph, g.GroupOpenGlyph, g.GroupClosedGlyph, g.ShellGlyph, g.LoaderHead, g.LoaderTail, g.LoaderRest}, g.StartupFrames...) {
				if ansi.StringWidth(mark) != 1 {
					t.Errorf("mark %q must occupy one cell", mark)
				}
				if mode == presentation.GlyphModeBasic && (len(mark) != 1 || mark[0] < '!' || mark[0] > '~') {
					t.Errorf("basic mark %q must be printable ASCII", mark)
				}
			}
			for toolName, tool := range cfg.Tools {
				m := &Model{services: services{cfg: cfg}, prefs: preferences{glyphMode: mode}}
				for _, state := range append(states, status.Dead) {
					sess := store.Session{Tool: toolName, Status: state}
					for phase := 0; phase < len(g.StartupFrames)*2; phase++ {
						m.startup.startupPhase = phase
						want := g.StatusGlyph(state)
						if state == status.Starting {
							want = g.StartupFrames[phase%len(g.StartupFrames)]
						} else if tool.Shell && state != status.Errored && state != status.Dead {
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
		if !strings.Contains(ansi.Strip(m.viewHelp()), m.glyphs().StatusGlyph(status.Working)+" working") {
			t.Fatal("help and renderer disagree")
		}
		ring := ansi.Strip(strings.Join(ringLoader(m.glyphs(), 40, 10, "loading", 0), "\n"))
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
