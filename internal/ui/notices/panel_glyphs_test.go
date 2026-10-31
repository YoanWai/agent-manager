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
	p := NewPanel(nil, nil)
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

func TestCompatibleModeUsesASCIIForEveryNewGlyph(t *testing.T) {
	want := map[presentation.GlyphMode][9]string{
		presentation.GlyphModeAdvanced: {"▲", "✧", "✚", "●", "◌", "◇", "♥", "▍", "●"},
		presentation.GlyphModeBasic:    {"!", "?", "+", "*", "-", "o", "&", "|", "*"},
	}
	for mode, glyphs := range want {
		g := presentation.GlyphsFor(mode)
		got := [9]string{g.HeadsUpGlyph, g.TipGlyph, g.FeatureGlyph, g.FixGlyph, g.OtherGlyph, g.StartedGlyph, g.PeopleGlyph, g.SectionBarGlyph, g.UnreadGlyph}
		if got != glyphs {
			t.Fatalf("%s glyphs = %q, want %q", mode, got, glyphs)
		}
	}

	src := inboxSources()
	src.Feed = append(src.Feed, Message{ID: "feed-tip", Kind: KindTip, Title: "Try the tip", Banner: "a quick one"})
	src.Update.Version = src.Update.Latest
	src.WhatsNewVersion, src.WhatsNewFrom = src.Update.Latest, "v0.45.0"
	src.ConfigImportError = "bad key"
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.See(inboxHeadsUp)
	notices := p.Active(src)
	// The card's own chrome draws box lines and an ellipsis in either mode;
	// the bottom edge's keys still name ↑↓, so it is left out.
	chrome := "─│█…"
	for _, width := range []int{100, 160} {
		for i := range notices {
			p.Open(notices, notices[i].ID)
			f := inboxFrame(width)
			f.GlyphMode = presentation.GlyphModeBasic
			view := p.View(notices, f)
			for _, line := range view[:len(view)-1] {
				for _, r := range ansi.Strip(line) {
					if r > 0x7f && !strings.ContainsRune(chrome, r) {
						t.Fatalf("width %d, %s: %q in the compatible view: %q", width, notices[i].ID, r, ansi.Strip(line))
					}
				}
			}
		}
	}
}
