package notices

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

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
				got := ansi.Cut(ansi.Strip(rows[len(rows)-1+hit.Y]), hit.X0, hit.X1)
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

// footRows is the content of the rows the footer stacked above the bottom
// edge, top to bottom, with the card's border and padding cut off.
func footRows(view []string, count int) []string {
	rows := make([]string, count)
	for i := range rows {
		row := ansi.Strip(view[len(view)-1-count+i])
		rows[i] = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(row, "│"), "│"))
	}
	return rows
}

func footNotices(id string) []Notice {
	if id == "" {
		return nil
	}
	return []Notice{{ID: id, Title: "notice", Body: []string{"content"}}}
}

func TestNoticeFooterPaintsEveryKeyWhereFootReports(t *testing.T) {
	for width := 10; width <= 60; width++ {
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			for _, id := range []string{"", "custom", "update-test"} {
				notices := footNotices(id)
				p := NewPanel(nil, nil)
				f := testFrame()
				f.Width, f.GlyphMode = width, mode
				view := p.View(notices, f)
				feet := p.Foot(notices, f)
				want := []string{"r", "esc/q"}
				if id != "" {
					want = append(want, "↵", "x")
				}
				if id == "update-test" {
					want = append(want, "u")
				}
				inner := innerWidth(notices, width)
				cut := false
				for _, label := range want {
					cut = cut || ansi.StringWidth(presentation.KeyLabel(mode, label)) > inner
				}
				labels := map[string]bool{}
				byRow := map[int][]string{}
				top := 0
				for _, foot := range feet {
					labels[foot.Label] = true
					top = min(top, foot.Y)
					row := len(view) - 1 + foot.Y
					if row < 1 || row > len(view)-1 {
						t.Fatalf("%s/%s/%d: %+v is off the card", mode, id, width, foot)
					}
					label := presentation.KeyLabel(mode, foot.Label)
					got := ansi.Cut(ansi.Strip(view[row]), foot.X0, foot.X1)
					if got != label && !wordedPart(got, label) && got != ansi.Truncate(label, inner, "…") {
						t.Errorf("%s/%s/%d: %+v paints %q, want %q", mode, id, width, foot, got, label)
					}
					if foot.Y < 0 && foot.X1 > footStart+inner {
						t.Errorf("%s/%s/%d: %+v reaches past the card's inner edge %d", mode, id, width, foot, footStart+inner)
					}
					byRow[foot.Y] = append(byRow[foot.Y], got)
				}
				for _, label := range want {
					if !labels[label] {
						t.Errorf("%s/%s/%d: footer lost %q, got %+v", mode, id, width, label, feet)
					}
				}
				for row := len(view) - 1 + top; row < len(view) && !cut; row++ {
					if strings.Contains(ansi.Strip(view[row]), "…") {
						t.Errorf("%s/%s/%d: footer row cut: %q", mode, id, width, ansi.Strip(view[row]))
					}
				}
				for i, got := range footRows(view, -top) {
					if want := strings.Join(byRow[i+top], footSep); got != want {
						t.Errorf("%s/%s/%d: stacked row %q, Foot reports %q", mode, id, width, got, want)
					}
				}
				edge := ansi.Strip(view[len(view)-1])
				if got, want := footerLegend(edge, f.GlyphMode), strings.Join(byRow[0], footSep); got != want {
					t.Errorf("%s/%s/%d: edge legend %q, Foot reports %q", mode, id, width, got, want)
				}
			}
		}
	}
}

// wordedPart reports whether an edge part reads as label, one space and a
// word, with no separator, cut mark or border inside it.
func wordedPart(part, label string) bool {
	word, ok := strings.CutPrefix(part, label+" ")
	if !ok || word == "" || strings.ContainsAny(part, "·…│─╰") {
		return false
	}
	for _, r := range word {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func longNotice() []Notice {
	body := make([]string, 30)
	for i := range body {
		body[i] = fmt.Sprintf("line %02d", i+1)
	}
	return []Notice{{ID: "custom", Title: "notice", Body: body}}
}

// The stacked rows join the tail in the body's budget, so the card keeps
// to the terminal and the last page still reaches the last body line.
func TestStackedNoticeFooterFollowsAResize(t *testing.T) {
	for _, id := range []string{"", "custom", "update-test"} {
		notices := footNotices(id)
		p := NewPanel(nil, nil)
		for _, width := range []int{80, 20, 13, 10, 15, 43, 13, 200} {
			f := testFrame()
			f.Width, f.GlyphMode = width, presentation.GlyphModeBasic
			fresh := NewPanel(nil, nil)
			if got, want := p.View(notices, f), fresh.View(notices, f); !slices.Equal(got, want) {
				t.Fatalf("%s/%d: resized view differs from a fresh one:\n%s\n%s", id, width, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if got, want := p.Foot(notices, f), fresh.Foot(notices, f); !slices.Equal(got, want) {
				t.Fatalf("%s/%d: resized foot %+v, fresh %+v", id, width, got, want)
			}
		}
	}
	notices := longNotice()
	p := NewPanel(nil, nil)
	narrow, wide := testFrame(), testFrame()
	narrow.Width, narrow.Height, narrow.GlyphMode = 20, 12, presentation.GlyphModeBasic
	wide.Width, wide.Height, wide.GlyphMode = 40, 12, presentation.GlyphModeBasic
	p.Key(key("end"), notices, narrow)
	if p.ScrollLimit(notices, wide) >= p.Offset() {
		t.Fatalf("test setup: the wide limit %d is not under the narrow offset %d", p.ScrollLimit(notices, wide), p.Offset())
	}
	p.Wheel(1, notices, wide)
	if p.Offset() != p.ScrollLimit(notices, wide) {
		t.Fatalf("after the resize the wheel moved to %d, limit %d", p.Offset(), p.ScrollLimit(notices, wide))
	}
}
