package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleNoticeFooterMeasuresDisplayedKeys(t *testing.T) {
	const literal = "↵⌫🬹🬂🬨 notice content"
	notices := []Notice{{ID: "custom", Title: literal, Body: []string{literal}, URL: "https://example.com"}}
	p := NewPanel(nil)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{66, 80, 120} {
			f := testFrame()
			f.Width = width
			f.GlyphMode = mode
			rows := p.View(notices, f)
			frame := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(frame, literal) {
				t.Fatal("notice content rewritten")
			}
			for _, hit := range p.Foot(notices, f) {
				got := ansi.Cut(ansi.Strip(rows[len(rows)-1]), hit.X0, hit.X1)
				want := presentation.KeyLabel(mode, hit.Label)
				if !strings.HasPrefix(got, want) {
					t.Fatalf("%s/%d label %q hit=%q", mode, width, hit.Label, got)
				}
			}
			if mode == presentation.GlyphModeBasic && strings.ContainsAny(ansi.Strip(rows[len(rows)-1]), "↵⌫") {
				t.Fatal("footer contains key glyphs")
			}
		}
	}
}
