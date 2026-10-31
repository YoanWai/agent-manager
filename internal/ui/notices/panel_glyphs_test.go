package notices

import (
	"strings"
	"testing"
	"time"

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
	src.Feed[1].URL, src.Feed[1].Action = "https://github.com/tmux/tmux/issues/4511", "Read the tmux issue"
	src.Feed[1].Expires = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	src.Feed = append(src.Feed, Message{ID: "feed-tip", Kind: KindTip, Title: "Try the tip", Banner: "a quick one", Body: []string{"Press s for Settings."}, URL: RepoURL + "#readme"})
	src.Update.Version = src.Update.Latest
	src.WhatsNewVersion, src.WhatsNewFrom = src.Update.Latest, "v0.45.0"
	src.ConfigImportError = "bad key"
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.See(inboxHeadsUp)
	notices := p.Active(src)
	// The card's own chrome draws box lines, an ellipsis and the middle dot
	// separator in either mode; the bottom edge's keys still name ↑↓, so it
	// is left out.
	chrome := "─│█…·"
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

func TestCompatibleNoticeChromeKeepsBodyAndScrollGeometry(t *testing.T) {
	const literal = "◂▸╭╮╰╯━╸┃┄▀▄▐▜▟"
	n := Notice{Kind: KindNews, Title: "Title", Body: []string{literal}}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		rows := feedPane(n, 80, testTheme(), mode)
		if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), literal) {
			t.Fatal("notice body was converted")
		}
		thumb := "█"
		if mode == presentation.GlyphModeAdvanced {
			thumb = "┃"
		}
		body := make([]string, 12)
		for i := range body {
			body[i] = "text"
		}
		for _, offset := range []int{0, 4, 8} {
			window := scrollWindow(body, 4, offset, 20, testTheme(), mode)
			if len(window) != 4 {
				t.Fatal("scroll height changed")
			}
			count := 0
			for _, row := range window {
				if ansi.StringWidth(row) != 20 {
					t.Fatal("scroll width changed")
				}
				if strings.HasSuffix(ansi.Strip(row), thumb) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("offset %d has %d thumbs", offset, count)
			}
		}
	}
}
