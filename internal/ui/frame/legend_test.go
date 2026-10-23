package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plainCap is the test palette: no color, so painted widths are readable.
func plainCap() Cap {
	return Cap{}
}

func plainTitle(s string) string { return s }

func TestPaintLegendKeepsTierTitles(t *testing.T) {
	out := ansi.Strip(PaintLegend([]Section{
		{Title: "Session", Pairs: [][2]string{{"↵", "attach"}}},
		{Title: "View", Quiet: true, Pairs: [][2]string{{"q", "quit"}}},
	}, 200, plainCap(), plainTitle, plainTitle, " · ").Text)
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
func TestPaintLegendCapsRowsAndMarksTheCut(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 40; i++ {
		pairs = append(pairs, [2]string{"k", "an action"})
	}
	out := PaintLegend([]Section{{Title: "Session", Pairs: pairs}}, 40, plainCap(), plainTitle, plainTitle, " · ").Text
	lines := strings.Split(out, "\n")
	if len(lines) > MaxRows {
		t.Fatalf("legend took %d rows, want at most %d", len(lines), MaxRows)
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

func TestPaintLegendReservesARowForTheNextTier(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 40; i++ {
		pairs = append(pairs, [2]string{"k", "an action"})
	}
	out := ansi.Strip(PaintLegend([]Section{
		{Title: "Session", Pairs: pairs},
		{Title: "View", Quiet: true, Pairs: [][2]string{{"M", "messages"}}},
	}, 30, plainCap(), plainTitle, plainTitle, " · ").Text)
	lines := strings.Split(out, "\n")
	if len(lines) != MaxRows {
		t.Fatalf("legend took %d rows, want %d:\n%s", len(lines), MaxRows, out)
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

func TestPaintLegendLeadsAlertsAndHints(t *testing.T) {
	out := ansi.Strip(PaintLegend([]Section{{
		Title:  "Session",
		Leads:  [][2]string{{"p", "quick prompt mode"}},
		Alerts: [][2]string{{"M", "unread"}},
		Hint:   "a hint",
		Pairs:  [][2]string{{"r", "rename"}},
	}}, 200, plainCap(), plainTitle, plainTitle, " · ").Text)
	for _, want := range []string{"p quick prompt mode", "M unread", "a hint", "r rename"} {
		if !strings.Contains(out, want) {
			t.Fatalf("legend lost %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "quick prompt mode") > strings.Index(out, "a hint") {
		t.Fatalf("the hint must sit after the leads:\n%s", out)
	}
}

func TestPaintInlineWrapsWithinItsWidth(t *testing.T) {
	pairs := [][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"o", "docs"}, {"esc", "cancel"}}
	for _, width := range []int{80, 14} {
		painted := PaintInline(pairs, width, plainCap())
		for _, line := range strings.Split(ansi.Strip(painted.Text), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width %d: inline row is %d columns: %q", width, w, line)
			}
		}
	}
}
