package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestNavigationConfiguredFooterLabelsKeepLiteralKeys(t *testing.T) {
	m := helpModel()
	for _, tc := range []struct{ key, basic string }{{"left", "left"}, {"shift+up", "shift+up"}, {"←", "←"}, {"↑", "↑"}, {"↕", "↕"}, {"v", "v"}, {"<", "<"}} {
		key, err := keybind.Parse(tc.key)
		if err != nil {
			t.Fatal(err)
		}
		m.services.listKeys = m.services.listKeys.With(keybind.Prompt, keybind.Keys(key))
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			m.prefs.glyphMode = mode
			want := key.Glyph()
			if mode == presentation.GlyphModeBasic {
				want = tc.basic
			}
			label := m.listGlyph(keybind.Prompt)
			if label != want {
				t.Fatalf("%s %q label=%q want=%q", mode, tc.key, label, want)
			}
			bar := paintLegend([]legendSection{{title: "Keys", pairs: [][2]string{{label, "prompt"}}}}, 100, mode)
			if !strings.Contains(ansi.Strip(bar.text), want+" prompt") {
				t.Fatal("second pass changed binding")
			}
		}
	}
}
