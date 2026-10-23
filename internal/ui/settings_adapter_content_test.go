package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleCardsKeepTitlesAndClickGeometry(t *testing.T) {
	m := buildModel(t)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{34, 80, 120} {
			if err := m.services.store.SetSetting(uisettings.GlyphModeSetting, string(mode)); err != nil {
				t.Fatal(err)
			}
			m.layout.width, m.layout.height = width, 24
			m.applyCmd(t, m.openSettings())
			m.settings.dialog.Field = uisettings.FieldGlyphs
			got := ansi.Strip(m.viewSettings())
			title, cursor := "# Settings", "> symbols"
			if mode == presentation.GlyphModeAdvanced {
				title, cursor = "⚙ Settings", "❯ symbols"
			}
			if !strings.Contains(got, title) || (width >= 36 && !strings.Contains(got, cursor)) {
				t.Fatalf("%s/%d Settings missing title or cursor:\n%s", mode, width, got)
			}
			m, _ = clickBody(t, m, "symbols")
			if m.prefs.glyphMode.Effective() == mode {
				t.Fatal("symbols click missed its painted row")
			}
			m.applyCmd(t, m.openSettings())
		}
	}
}
