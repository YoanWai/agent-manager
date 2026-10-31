package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleDialogHintsPreserveContentAndModelIsolation(t *testing.T) {
	const literal = "↵⌫🬹🬂🬨 user content"
	basic, advanced := shotModel(), shotModel()
	basic.prefs.glyphMode = presentation.GlyphModeBasic
	advanced.prefs.glyphMode = presentation.GlyphModeAdvanced
	for _, m := range []*Model{basic, advanced, basic} {
		got := ansi.Strip(m.CardSized(m.cardWidth(), "title", literal, [][2]string{{"↵", "save"}, {"esc", "cancel"}}))
		if !strings.Contains(got, literal) {
			t.Fatalf("content changed: %s", got)
		}
		want := presentation.KeyLabel(m.prefs.glyphMode, "↵") + " save"
		if !strings.Contains(got, want) {
			t.Fatalf("model hint missing %q: %s", want, got)
		}
	}
}
