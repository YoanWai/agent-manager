package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestGlyphModesCoverEveryBuiltinTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []glyphMode{glyphModeBasic, glyphModeAdvanced} {
		t.Run(string(mode), func(t *testing.T) {
			g := glyphsFor(mode)
			seen := map[string]string{}
			states := []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Idle}
			for _, state := range states {
				mark := g.statusGlyph(state)
				if other, ok := seen[mark]; ok {
					t.Errorf("%s and %s share %q", other, state, mark)
				}
				seen[mark] = state
				if mode == glyphModeBasic && (len(mark) != 1 || mark[0] < '!' || mark[0] > '~') {
					t.Errorf("basic %s mark %q must be printable ASCII", state, mark)
				}
				if ansi.StringWidth(mark) != 1 {
					t.Errorf("%s mark %q must occupy one cell", state, mark)
				}
			}
			if g.statusGlyph(status.Dead) != g.statusGlyph(status.Errored) {
				t.Fatal("dead sessions lost their error mark")
			}
			for _, mark := range append([]string{g.reorderGrip, g.inboxGlyph, g.branchGlyph, g.groupOpenGlyph, g.groupClosedGlyph, g.shellGlyph, g.loaderHead, g.loaderTail, g.loaderRest}, g.startupFrames...) {
				if ansi.StringWidth(mark) != 1 {
					t.Errorf("mark %q must occupy one cell", mark)
				}
				if mode == glyphModeBasic && (len(mark) != 1 || mark[0] < '!' || mark[0] > '~') {
					t.Errorf("basic mark %q must be printable ASCII", mark)
				}
			}
			for toolName, tool := range cfg.Tools {
				m := &Model{cfg: cfg, glyphMode: mode}
				for _, state := range append(states, status.Dead) {
					sess := store.Session{Tool: toolName, Status: state}
					for phase := 0; phase < len(g.startupFrames)*2; phase++ {
						m.startupPhase = phase
						want := g.statusGlyph(state)
						if state == status.Starting {
							want = g.startupFrames[phase%len(g.startupFrames)]
						} else if tool.Shell && state != status.Errored && state != status.Dead {
							want = g.shellGlyph
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
	basic, advanced := &Model{glyphMode: glyphModeBasic}, &Model{glyphMode: glyphModeAdvanced}
	want := map[string]string{status.Working: "◐", status.Starting: "◌", status.Waiting: "◆", status.Finished: "●", status.Errored: "✕", status.Idle: "○"}
	for state, mark := range want {
		if got := advanced.glyphs().statusGlyph(state); got != mark {
			t.Errorf("advanced %s = %q want %q", state, got, mark)
		}
		if basic.glyphs().statusGlyph(state) == mark {
			t.Errorf("advanced choice leaked into basic model for %s", state)
		}
	}
	g := advanced.glyphs()
	if g.reorderGrip != "⠿" || g.inboxGlyph != "✉" || g.branchGlyph != "⑂" || g.groupOpenGlyph != "▾" || g.groupClosedGlyph != "▸" {
		t.Fatalf("advanced controls changed: %+v", g)
	}
	for _, m := range []*Model{basic, advanced} {
		rows := m.glyphs().markHelpRows(m.listKeys)
		if rows[0][0] != m.glyphs().statusGlyph(status.Working)+" working" {
			t.Fatal("help and renderer disagree")
		}
		ring := ansi.Strip(strings.Join(m.glyphs().ringLoader(40, 10, "loading", 0), "\n"))
		for _, mark := range []string{m.glyphs().loaderHead, m.glyphs().loaderTail, m.glyphs().loaderRest} {
			if !strings.Contains(ring, mark) {
				t.Errorf("loader missing %q: %s", mark, ring)
			}
		}
	}
}

func TestGlyphPreferenceReadAndWriteErrorsSurface(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(glyphModeSetting, "unknown"); err != nil {
		t.Fatal(err)
	}
	m.handleMsg(m.loadGlyphMode()())
	if m.glyphMode != "" || !strings.Contains(m.errBar.text, "unknown symbols setting") {
		t.Fatalf("invalid external preference: mode=%q error=%q", m.glyphMode, m.errBar.text)
	}
	m.store.Close()
	m.handleMsg(m.loadGlyphMode()())
	if !strings.Contains(m.errBar.text, "reading symbols setting") {
		t.Fatal("read failure swallowed")
	}
	m.glyphMode = glyphModeAdvanced
	m.settings.glyphModeChanged = true
	cmd := m.saveGlyphMode(false)
	m.handleMsg(cmd())
	if !strings.Contains(m.errBar.text, "saving symbols setting") {
		t.Fatal("write failure swallowed")
	}
}
