package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/charmbracelet/x/ansi"
)

func TestLegendBarKeepsTierTitles(t *testing.T) {
	out := ansi.Strip(paintLegend([]legendSection{
		{title: "Session", pairs: [][2]string{{"↵", "attach"}}},
		{title: "View", quiet: true, pairs: [][2]string{{"q", "quit"}}},
	}, 200).text)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("one line per tier, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "Session") || !strings.Contains(lines[0], "attach") {
		t.Fatalf("first tier should name itself and its keys: %q", lines[0])
	}
	if !strings.Contains(lines[1], "View") || !strings.Contains(lines[1], "quit") {
		t.Fatalf("second tier should name itself and its keys: %q", lines[1])
	}
}

// A footer that wraps without limit steals the rows the preview needs, so
// the tail is cut and marked instead.
func TestLegendBarCapsRowsAndMarksTheCut(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 40; i++ {
		pairs = append(pairs, [2]string{"k", "an action"})
	}
	out := paintLegend([]legendSection{{title: "Session", pairs: pairs}}, 40).text
	lines := strings.Split(out, "\n")
	if len(lines) > legendMaxRows {
		t.Fatalf("legend took %d rows, want at most %d", len(lines), legendMaxRows)
	}
	if !strings.Contains(ansi.Strip(out), "…") {
		t.Fatalf("a cut legend must say so:\n%s", ansi.Strip(out))
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > 40 {
			t.Fatalf("wrapped legend row is %d columns wide, budget is 40: %q", w, ansi.Strip(line))
		}
	}
}

func TestLegendBarReservesRowForNextTier(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 40; i++ {
		pairs = append(pairs, [2]string{"k", "an action"})
	}
	out := ansi.Strip(paintLegend([]legendSection{
		{title: "Session", pairs: pairs},
		{title: "View", quiet: true, pairs: [][2]string{{"M", "messages"}}},
	}, 30).text)
	lines := strings.Split(out, "\n")
	if len(lines) != legendMaxRows {
		t.Fatalf("legend took %d rows, want %d:\n%s", len(lines), legendMaxRows, out)
	}
	if !strings.Contains(lines[1], "…") {
		t.Fatalf("crowded first tier must mark its cut:\n%s", out)
	}
	if !strings.Contains(lines[2], "View") || !strings.Contains(lines[2], "M messages") {
		t.Fatalf("the next tier must remain visible:\n%s", out)
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > 30 {
			t.Fatalf("legend row is %d columns wide, budget is 30: %q", w, line)
		}
	}
}

func TestViewLegendPaintsWaitingMessagesWarm(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	waiting := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		update:   updateInfo{latest: "v0.41.0", version: "0.40.0"},
	}
	bar := paintLegend([]legendSection{waiting.viewLegend()}, 160).text
	if !strings.Contains(bar, legendAlertStyle().Render("M unread")) {
		t.Fatalf("waiting messages should use the working color:\n%s", bar)
	}
	idle := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		update:   updateInfo{version: "0.40.0"},
		notices:  noticesPanel{Panel: uinotices.NewPanel(map[string]bool{uinotices.Welcome: true, uinotices.ArrowStep: true})},
	}
	idleBar := ansi.Strip(paintLegend([]legendSection{idle.viewLegend()}, 160).text)
	if strings.Contains(idleBar, "unread") {
		t.Fatalf("an empty panel should leave the messages key off:\n%s", idleBar)
	}
}

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
		key, ok := legendKey(label)
		if !ok || key.String() != want {
			t.Errorf("legendKey(%q) = %q ok=%v, want %q", label, key.String(), ok, want)
		}
	}
}

// Movement belongs to the rows and the wheel, and a gesture names no key.
func TestLegendKeyLeavesMovesAndGesturesAlone(t *testing.T) {
	for _, label := range []string{
		"↑↓", "←→", "←", "tab/↑↓", "↑↓/jk", "←→/click", "pgup/pgdn", "pgup/pgdn/home/end",
		"type", "any key", "click", "double click", "drag", "release", "drag / ↑↓ / wheel", "click / alt+drag",
	} {
		if key, ok := legendKey(label); ok {
			t.Errorf("legendKey(%q) pressed %q, want nothing", label, key.String())
		}
	}
}

// A hit spans exactly the pair it was painted for, on its own row, so a
// click reads the same columns the eye does.
func TestPaintedLegendHitsCoverTheirPairs(t *testing.T) {
	sections := []legendSection{
		{title: "Session", leads: [][2]string{{"p", "quick prompt mode"}}, hint: "a hint", pairs: [][2]string{{"↵", "focus"}, {"click", "focus"}, {"x/X", "kill / all"}, {"r", "rename"}, {"m", "move"}}},
		{title: "View", quiet: true, alerts: [][2]string{{"M", "unread"}}, pairs: [][2]string{{"↑ ↓", "navigate"}, {"n", "new"}, {"/", "search"}, {"?", "keys"}, {"q", "quit"}}},
	}
	drawn := map[string]string{
		"p": "p quick prompt mode", "enter": "↵ focus", "x": "x/X kill / all", "r": "r rename", "m": "m move",
		"M": "M unread", "n": "n new", "/": "/ search", "?": "? keys", "q": "q quit",
	}
	for _, width := range []int{200, 48} {
		painted := paintLegend(sections, width)
		lines := strings.Split(ansi.Strip(painted.text), "\n")
		got := map[string]bool{}
		for _, hit := range painted.hits {
			cells := []rune(lines[hit.y])
			if hit.x1 > len(cells) {
				t.Fatalf("width %d: hit %+v runs past its row %q", width, hit, lines[hit.y])
			}
			label := strings.TrimSpace(string(cells[hit.x0:hit.x1]))
			if label != drawn[hit.key.String()] {
				t.Errorf("width %d: hit for %q covers %q, want %q", width, hit.key.String(), label, drawn[hit.key.String()])
			}
			got[hit.key.String()] = true
		}
		for key, label := range drawn {
			if shown := strings.Contains(ansi.Strip(painted.text), label); got[key] != shown {
				t.Errorf("width %d: %q painted=%v but hit=%v", width, key, shown, got[key])
			}
		}
	}
}

func TestPaintedInlineHitsCoverTheirPairs(t *testing.T) {
	pairs := [][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"o", "docs"}, {"esc", "cancel"}}
	for _, width := range []int{80, 14} {
		painted := paintInline(pairs, width)
		lines := strings.Split(ansi.Strip(painted.text), "\n")
		want := map[string]string{"enter": "↵ move", "o": "o docs", "esc": "esc cancel"}
		if len(painted.hits) != len(want) {
			t.Fatalf("width %d: hits %+v, want one per key pair", width, painted.hits)
		}
		for _, hit := range painted.hits {
			label := strings.TrimSpace(string([]rune(lines[hit.y])[hit.x0:hit.x1]))
			if label != want[hit.key.String()] {
				t.Errorf("width %d: hit for %q covers %q, want %q", width, hit.key.String(), label, want[hit.key.String()])
			}
		}
	}
}
