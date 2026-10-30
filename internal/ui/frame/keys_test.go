package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLegendKeyPressesTheFirstKeyItNames(t *testing.T) {
	for label, want := range map[string]string{
		"n":                                    "n",
		"↵":                                    "enter",
		"y/↵":                                  "y",
		"n/esc":                                "n",
		"esc/q":                                "esc",
		"↵/tab":                                "enter",
		"space/↵":                              " ",
		"/":                                    "/",
		"x/X":                                  "x",
		"g/G":                                  "g",
		"o/f3":                                 "o",
		"ctrl+t":                               "ctrl+t",
		"alt+m":                                "alt+m",
		"tab":                                  "tab",
		`ctrl+\ / click the list / mouse back`: `ctrl+\`,
		"r / release":                          "r",
		"/ / ctrl+f":                           "/",
		"//ctrl+f":                             "/",
		"ctrl+f / /":                           "ctrl+f",
		"ctrl+f//":                             "ctrl+f",
		"alt+/":                                "alt+/",
	} {
		key, ok := LegendKey(label)
		if !ok || key.String() != want {
			t.Errorf("LegendKey(%q) = %q ok=%v, want %q", label, key.String(), ok, want)
		}
	}
}

// Movement belongs to the rows and the wheel, and a gesture names no key.
func TestLegendKeyLeavesMovesAndGesturesAlone(t *testing.T) {
	for _, label := range []string{
		"↑↓", "←→", "←", "tab/↑↓", "↑↓/jk", "←→/click", "pgup/pgdn", "pgup/pgdn/home/end",
		"type", "any key", "click", "double click", "drag", "release", "drag / ↑↓ / wheel", "click / alt+drag",
	} {
		if key, ok := LegendKey(label); ok {
			t.Errorf("LegendKey(%q) pressed %q, want nothing", label, key.String())
		}
	}
}

// A hit spans exactly the pair it was painted for, on its own row, so a
// click reads the same columns the eye does.
func TestPaintedLegendHitsCoverTheirPairs(t *testing.T) {
	sections := []Section{
		{Title: "Session", Leads: [][2]string{{"p", "quick prompt mode"}}, Hint: "a hint", Pairs: [][2]string{{"↵", "focus"}, {"click", "focus"}, {"x/X", "kill / all"}, {"r", "rename"}, {"m", "move"}}},
		{Title: "View", Quiet: true, Alerts: [][2]string{{"M", "unread"}}, Pairs: [][2]string{{"↑ ↓", "navigate"}, {"n", "new"}, {"/", "search"}, {"?", "keys"}, {"q", "quit"}}},
	}
	drawn := map[string]string{
		"p": "p quick prompt mode", "enter": "↵ focus", "x": "x/X kill / all", "r": "r rename", "m": "m move",
		"M": "M unread", "n": "n new", "/": "/ search", "?": "? keys", "q": "q quit",
	}
	for _, width := range []int{200, 48} {
		painted := PaintLegend(sections, width, plainCap(), plainTitle, plainTitle, " · ")
		lines := ansiTextRows(painted.Text)
		got := map[string]bool{}
		for _, hit := range painted.Hits {
			cells := []rune(lines[hit.Y])
			if hit.X1 > len(cells) {
				t.Fatalf("width %d: hit %+v runs past its row %q", width, hit, lines[hit.Y])
			}
			label := strings.TrimSpace(string(cells[hit.X0:hit.X1]))
			if label != drawn[hit.Key.String()] {
				t.Errorf("width %d: hit for %q covers %q, want %q", width, hit.Key.String(), label, drawn[hit.Key.String()])
			}
			got[hit.Key.String()] = true
		}
		for key, label := range drawn {
			if shown := strings.Contains(ansi.Strip(painted.Text), label); got[key] != shown {
				t.Errorf("width %d: %q painted=%v but hit=%v", width, key, shown, got[key])
			}
		}
	}
}

func TestPaintedInlineHitsCoverTheirPairs(t *testing.T) {
	pairs := [][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"o", "docs"}, {"esc", "cancel"}}
	for _, width := range []int{80, 14} {
		painted := PaintInline(pairs, width, plainCap())
		lines := ansiTextRows(painted.Text)
		want := map[string]string{"enter": "↵ move", "o": "o docs", "esc": "esc cancel"}
		if len(painted.Hits) != len(want) {
			t.Fatalf("width %d: hits %+v, want one per key pair", width, painted.Hits)
		}
		for _, hit := range painted.Hits {
			label := strings.TrimSpace(string([]rune(lines[hit.Y])[hit.X0:hit.X1]))
			if label != want[hit.Key.String()] {
				t.Errorf("width %d: hit for %q covers %q, want %q", width, hit.Key.String(), label, want[hit.Key.String()])
			}
		}
	}
}

// ansiTextRows is a painted block's rows, in display cells.
func ansiTextRows(text string) []string { return strings.Split(text, "\n") }
