package notices

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Past the width the bottom edge holds, the keys stack inside the card:
// with their words while each fits the card, bare below that, and cut at
// the card's inner edge only when a key alone is wider than the card.
func TestNoticeFooterAtNarrowWidths(t *testing.T) {
	basic, advanced := presentation.GlyphModeBasic, presentation.GlyphModeAdvanced
	for _, tc := range []struct {
		width int
		mode  presentation.GlyphMode
		id    string
		stack []string
	}{
		{20, advanced, "", []string{"r refresh", "esc/q"}},
		{20, advanced, "custom", []string{"r refresh", "↵ open", "x dismiss", "esc/q"}},
		{20, basic, "update-test", []string{"r refresh", "u update", "enter open", "x dismiss", "esc/q"}},
		{15, advanced, "", []string{"r", "esc/q"}},
		{15, advanced, "custom", []string{"r · ↵", "x", "esc/q"}},
		{15, advanced, "update-test", []string{"r · u", "↵ · x", "esc/q"}},
		{15, basic, "custom", []string{"r", "enter", "x", "esc/q"}},
		{15, basic, "update-test", []string{"r · u", "enter", "x", "esc/q"}},
		{14, advanced, "custom", []string{"r · ↵", "x", "esc/q"}},
		{14, basic, "custom", []string{"r", "enter", "x", "esc/q"}},
		{13, advanced, "update-test", []string{"r · u", "↵ · x", "esc/q"}},
		{13, basic, "update-test", []string{"r · u", "enter", "x", "esc/q"}},
		{12, advanced, "custom", []string{"r", "↵", "x", "esc…"}},
		{12, basic, "custom", []string{"r", "ent…", "x", "esc…"}},
		{12, basic, "update-test", []string{"r", "u", "ent…", "x", "esc…"}},
		{11, advanced, "update-test", []string{"r", "u", "↵", "x", "es…"}},
		{11, basic, "custom", []string{"r", "en…", "x", "es…"}},
		{10, advanced, "", []string{"r", "e…"}},
		{10, advanced, "custom", []string{"r", "↵", "x", "e…"}},
		{10, basic, "custom", []string{"r", "e…", "x", "e…"}},
		{10, basic, "update-test", []string{"r", "u", "e…", "x", "e…"}},
	} {
		t.Run(fmt.Sprintf("%s/%s/%d", tc.mode, tc.id, tc.width), func(t *testing.T) {
			notices := footNotices(tc.id)
			p := NewPanel(nil)
			f := testFrame()
			f.Width, f.GlyphMode = tc.width, tc.mode
			view := p.View(notices, f)
			if want := plainFooter(tc.width, tc.mode); ansi.Strip(view[len(view)-1]) != want {
				t.Fatalf("edge = %q, want the plain border", ansi.Strip(view[len(view)-1]))
			}
			if got := footRows(view, len(tc.stack)); !slices.Equal(got, tc.stack) {
				t.Fatalf("stacked rows = %q, want %q:\n%s", got, tc.stack, ansi.Strip(strings.Join(view, "\n")))
			}
			inner := innerWidth(notices, tc.width)
			for _, foot := range p.Foot(notices, f) {
				if foot.Y >= 0 || foot.X0 < footStart || foot.X1 > footStart+inner || foot.X0 >= foot.X1 {
					t.Errorf("%+v is not inside the card's %d inner columns", foot, inner)
				}
			}
		})
	}
}

// As the terminal narrows the footer only steps down: the edge with
// navigation, the edge with the actions, the actions stacked with their
// words, then bare. No width puts bare keys on the edge.
func TestNoticeFooterDegradesInOneDirection(t *testing.T) {
	widths := []int{200, 120, 80}
	for width := 60; width >= 10; width-- {
		widths = append(widths, width)
	}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, id := range []string{"", "custom", "update-test"} {
			notices := footNotices(id)
			step := 0
			for _, width := range widths {
				edge, stack := footLayout(notices, 0, innerWidth(notices, width), mode, testFrame().CloseKey)
				level := 3
				switch {
				case len(edge) > 0 && edge[0][0] == "↑↓":
					level = 0
				case len(edge) > 0:
					level = 1
				case slices.ContainsFunc(stack, func(row [][2]string) bool {
					return slices.ContainsFunc(row, func(pair [2]string) bool { return pair[1] != "" })
				}):
					level = 2
				}
				if level < step {
					t.Fatalf("%s/%s: width %d stepped back up to %d from %d", mode, id, width, level, step)
				}
				step = level
				for _, pair := range edge {
					if pair[1] == "" && pair[0] != "esc/q" {
						t.Fatalf("%s/%s/%d: the edge names %q bare: %q", mode, id, width, pair[0], edge)
					}
				}
			}
			if step != 3 {
				t.Fatalf("%s/%s: the footer never went bare by width 10", mode, id)
			}
		}
	}
}

// Wide terminals keep the edge they had.
func TestNoticeFooterKeepsWideLayouts(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		for _, tc := range []struct {
			id, want string
		}{
			{"", "r refresh · esc/q"},
			{"custom", "r refresh · ↵ open · x dismiss · esc/q"},
		} {
			p := NewPanel(nil)
			f := testFrame()
			f.Width, f.GlyphMode = width, presentation.GlyphModeAdvanced
			view := p.View(footNotices(tc.id), f)
			edge := ansi.Strip(view[len(view)-1])
			if got := footerLegend(edge, f.GlyphMode); got != tc.want {
				t.Errorf("%s/%d: edge = %q, want %q", tc.id, width, got, tc.want)
			}
		}
	}
}

// The stack depends on the width alone, so a short terminal never reflows
// it; the card gives up body rows instead, down to a floor that holds the
// stack whole.
func TestNoticeFooterStackHoldsAtShortHeights(t *testing.T) {
	for _, width := range []int{10, 13, 15, 20, 25, 30, 40, 42} {
		for _, id := range []string{"", "custom", "update-test"} {
			notices := footNotices(id)
			p := NewPanel(nil)
			f := testFrame()
			f.Width, f.GlyphMode, f.Height = width, presentation.GlyphModeBasic, 4
			floor := len(p.View(notices, f))
			feet := p.Foot(notices, f)
			for height := 40; height >= 4; height-- {
				f.Height = height
				view := p.View(notices, f)
				if len(view) > max(height, floor) {
					t.Fatalf("%s/%d: the card paints %d rows into %d, floor %d", id, width, len(view), height, floor)
				}
				if got := p.Foot(notices, f); !slices.Equal(got, feet) {
					t.Fatalf("%s/%d/%d: foot %+v, want %+v", id, width, height, got, feet)
				}
				if !strings.HasPrefix(ansi.Strip(view[len(view)-1]), presentation.ChromeFor(f.GlyphMode).BottomLeftGlyph) {
					t.Fatalf("%s/%d/%d: the last row is not the bottom edge", id, width, height)
				}
			}
		}
	}
}

func TestStackedNoticeFooterKeepsConfiguredCloseKeys(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, binding := range []string{"M", "alt+m", "backspace"} {
			for _, width := range []int{13, 20, 40, 120} {
				for _, id := range []string{"", "custom"} {
					f := testFrame()
					f.Width, f.GlyphMode, f.CloseKey = width, mode, bindingOf(t, binding)
					p := NewPanel(nil)
					notices := footNotices(id)
					view := p.View(notices, f)
					label := "esc/q/" + f.CloseKey.Keys()[0].Glyph()
					found := false
					for _, hit := range p.Foot(notices, f) {
						if hit.Label != label {
							continue
						}
						found = true
						row := len(view) - 1 + hit.Y
						got := ansi.Cut(ansi.Strip(view[row]), hit.X0, hit.X1)
						want := ansi.Truncate(presentation.KeyLabel(mode, label), innerWidth(notices, width), "…")
						if got != want {
							t.Fatalf("%s/%s/%d close target paints %q, want %q", mode, binding, width, got, want)
						}
					}
					if !found {
						t.Fatalf("%s/%s/%d: no close target", mode, binding, width)
					}
					key := f.CloseKey.Keys()[0]
					msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}}
					if key.Tea() == "alt+m" {
						msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true}
					}
					if key.Tea() == "backspace" {
						msg = tea.KeyMsg{Type: tea.KeyBackspace}
					}
					if out := p.Key(msg, notices, f); out.Action != Close {
						t.Fatalf("%s did not close Messages", binding)
					}
				}
			}
		}
	}
}
