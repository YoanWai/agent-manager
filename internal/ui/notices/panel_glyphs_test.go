package notices

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The actions keep their words on the bottom edge down to the width that
// holds them, and one column under it they stack inside the card, words
// and all, rather than drop to bare keys on the edge.
func TestNoticeFooterKeepsActionKeysAtNarrowWidths(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      presentation.GlyphMode
		id        string
		edgeWidth int
		edge      string
		labels    []string
		stack     []string
	}{
		{"compatible", presentation.GlyphModeBasic, "custom", 49, "r refresh · enter open · x dismiss · esc/q", []string{"r", "↵", "x", "esc/q"}, []string{"r refresh · enter open · x dismiss", "esc/q"}},
		{"compatible-update", presentation.GlyphModeBasic, "update-test", 60, "r refresh · u update · enter open · x dismiss · esc/q", []string{"r", "u", "↵", "x", "esc/q"}, []string{"r refresh · u update · enter open · x dismiss", "esc/q"}},
		{"advanced", presentation.GlyphModeAdvanced, "custom", 45, "r refresh · ↵ open · x dismiss · esc/q", []string{"r", "↵", "x", "esc/q"}, []string{"r refresh · ↵ open · x dismiss", "esc/q"}},
		{"advanced-update", presentation.GlyphModeAdvanced, "update-test", 56, "r refresh · u update · ↵ open · x dismiss · esc/q", []string{"r", "u", "↵", "x", "esc/q"}, []string{"r refresh · u update · ↵ open · x dismiss", "esc/q"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notices := []Notice{{ID: tc.id, Title: "notice", Body: []string{"content"}}}
			p := NewPanel(nil)
			f := testFrame()
			f.Width, f.GlyphMode = tc.edgeWidth, tc.mode
			rows := p.View(notices, f)
			edge := ansi.Strip(rows[len(rows)-1])
			if got := footerLegend(edge, f.GlyphMode); got != tc.edge {
				t.Fatalf("footer = %q, want %q", got, tc.edge)
			}
			hits := p.Foot(notices, f)
			if len(hits) != len(tc.labels) {
				t.Fatalf("footer hit regions = %+v, want labels %v", hits, tc.labels)
			}
			for i, hit := range hits {
				label := presentation.KeyLabel(tc.mode, tc.labels[i])
				if got := ansi.Cut(edge, hit.X0, hit.X1); hit.Label != tc.labels[i] || hit.Y != 0 || (got != label && !wordedPart(got, label)) {
					t.Errorf("hit %+v paints %q, want %q and its word", hit, got, label)
				}
			}

			f.Width = tc.edgeWidth - 1
			rows = p.View(notices, f)
			if want := plainFooter(f.Width, f.GlyphMode); ansi.Strip(rows[len(rows)-1]) != want {
				t.Fatalf("width %d: edge = %q, want the plain border", f.Width, ansi.Strip(rows[len(rows)-1]))
			}
			if got := footRows(rows, len(tc.stack)); !slices.Equal(got, tc.stack) {
				t.Fatalf("width %d: stacked rows = %q, want %q", f.Width, got, tc.stack)
			}
		})
	}
}

func TestEmptyNoticeFooterRetainsRefreshHint(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		f := testFrame()
		f.Width, f.GlyphMode = 26, mode
		p := NewPanel(nil)
		rows := p.View(nil, f)
		if got := ansi.Strip(rows[len(rows)-1]); footerLegend(got, mode) != "r refresh · esc/q" {
			t.Fatalf("%s empty footer = %q", mode, got)
		}
	}
}

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

func TestCompatibleWelcomeFormatsRemappedKeys(t *testing.T) {
	src := testSources("")
	src.GlyphMode = presentation.GlyphModeBasic
	src.ListKeys = src.ListKeys.With(keybind.Settings, bindingOf(t, "backspace"))
	body := strings.Join(src.welcomeBody(), "\n")
	if strings.ContainsAny(body, "↵⌫") || !strings.Contains(body, "enter") || !strings.Contains(body, "backspace") {
		t.Fatalf("welcome key labels=%s", body)
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

func TestNoticeFooterStacksKeysPastTheEdge(t *testing.T) {
	basic, advanced := presentation.GlyphModeBasic, presentation.GlyphModeAdvanced
	for _, tc := range []struct {
		mode  presentation.GlyphMode
		id    string
		width int
		edge  string
		stack []string
	}{
		{basic, "custom", 20, "", []string{"r refresh", "enter open", "x dismiss", "esc/q"}},
		{basic, "update-test", 20, "", []string{"r refresh", "u update", "enter open", "x dismiss", "esc/q"}},
		{advanced, "custom", 20, "", []string{"r refresh", "↵ open", "x dismiss", "esc/q"}},
		{advanced, "update-test", 20, "", []string{"r refresh", "u update", "↵ open", "x dismiss", "esc/q"}},
		{basic, "custom", 13, "", []string{"r", "enter", "x", "esc/q"}},
		{advanced, "custom", 13, "", []string{"r · ↵", "x", "esc/q"}},
		{advanced, "custom", 14, "", []string{"r · ↵", "x", "esc/q"}},
		{basic, "", 13, "", []string{"r", "esc/q"}},
		{advanced, "", 13, "", []string{"r", "esc/q"}},
		{basic, "", 14, "", []string{"r", "esc/q"}},
		{advanced, "", 14, "", []string{"r", "esc/q"}},
		{basic, "", 20, "", []string{"r refresh", "esc/q"}},
		{advanced, "", 20, "", []string{"r refresh", "esc/q"}},
		{basic, "", 25, "r refresh · esc/q", nil},
		{advanced, "", 25, "r refresh · esc/q", nil},
	} {
		t.Run(fmt.Sprintf("%s/%s/%d", tc.mode, tc.id, tc.width), func(t *testing.T) {
			notices := footNotices(tc.id)
			p := NewPanel(nil)
			f := testFrame()
			f.Width, f.GlyphMode = tc.width, tc.mode
			view := p.View(notices, f)
			edge := ansi.Strip(view[len(view)-1])
			if got := footerLegend(edge, f.GlyphMode); tc.edge != "" && got != tc.edge {
				t.Fatalf("edge = %q, want legend %q", edge, tc.edge)
			}
			if want := plainFooter(tc.width, tc.mode); tc.edge == "" && edge != want {
				t.Fatalf("edge = %q, want the plain border %q", edge, want)
			}
			if got := footRows(view, len(tc.stack)); !slices.Equal(got, tc.stack) {
				t.Fatalf("stacked rows = %q, want %q:\n%s", got, tc.stack, ansi.Strip(strings.Join(view, "\n")))
			}
			if len(tc.stack) > 0 && len(notices) == 0 {
				if above := footRows(view, len(tc.stack)+1)[0]; above != "" {
					t.Fatalf("an empty panel's stack follows a blank row, got %q", above)
				}
			}
		})
	}
}

// Every key a click can run is painted, on the bottom edge or the rows
// stacked above it, exactly where Foot says, at every width from 10. Only a
// key wider than the card is cut, and its click stops at the card's edge.
func TestNoticeFooterPaintsEveryKeyWhereFootReports(t *testing.T) {
	for width := 10; width <= 60; width++ {
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			for _, id := range []string{"", "custom", "update-test"} {
				notices := footNotices(id)
				p := NewPanel(nil)
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
func TestStackedNoticeFooterKeepsTheBodyBudget(t *testing.T) {
	notices := longNotice()
	// Two edges, the blank row, one title, the separator, one body row,
	// the tail's spacer and the four stacked rows.
	const minimum = 11
	for height := minimum; height <= minimum+12; height++ {
		p := NewPanel(nil)
		f := testFrame()
		f.Width, f.Height, f.GlyphMode = 20, height, presentation.GlyphModeBasic
		view := p.View(notices, f)
		if len(view) > height {
			t.Fatalf("height %d: the card paints %d rows", height, len(view))
		}
		if !strings.HasPrefix(ansi.Strip(view[len(view)-1]), presentation.ChromeFor(f.GlyphMode).BottomLeftGlyph) {
			t.Fatalf("height %d: the last row is not the bottom edge", height)
		}
		if got := footRows(view, 4); !slices.Equal(got, []string{"r refresh", "enter open", "x dismiss", "esc/q"}) {
			t.Fatalf("height %d: stacked rows = %q", height, got)
		}
		if strings.Contains(ansi.Strip(strings.Join(view, "\n")), "line 30") {
			t.Fatalf("height %d: test setup: the body fits without scrolling", height)
		}
		p.Key(key("end"), notices, f)
		if p.Offset() == 0 || p.Offset() != p.ScrollLimit(notices, f) {
			t.Fatalf("height %d: end scrolled to %d, limit %d", height, p.Offset(), p.ScrollLimit(notices, f))
		}
		view = p.View(notices, f)
		// The last body line sits above the tail's spacer and the stack.
		if got := footRows(view, 6)[0]; !strings.Contains(got, "line 30") {
			t.Fatalf("height %d: the last page ends on %q:\n%s", height, got, ansi.Strip(strings.Join(view, "\n")))
		}
		p.Key(tea.KeyMsg{Type: tea.KeyPgDown}, notices, f)
		p.Wheel(1, notices, f)
		if p.Offset() != p.ScrollLimit(notices, f) {
			t.Fatalf("height %d: scrolling past the end moved to %d", height, p.Offset())
		}
	}
}

func TestStackedNoticeFooterFollowsAResize(t *testing.T) {
	for _, id := range []string{"", "custom", "update-test"} {
		notices := footNotices(id)
		p := NewPanel(nil)
		for _, width := range []int{80, 20, 13, 10, 15, 43, 13, 200} {
			f := testFrame()
			f.Width, f.GlyphMode = width, presentation.GlyphModeBasic
			fresh := NewPanel(nil)
			if got, want := p.View(notices, f), fresh.View(notices, f); !slices.Equal(got, want) {
				t.Fatalf("%s/%d: resized view differs from a fresh one:\n%s\n%s", id, width, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if got, want := p.Foot(notices, f), fresh.Foot(notices, f); !slices.Equal(got, want) {
				t.Fatalf("%s/%d: resized foot %+v, fresh %+v", id, width, got, want)
			}
		}
	}
	notices := longNotice()
	p := NewPanel(nil)
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
