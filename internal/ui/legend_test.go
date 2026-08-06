package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLegendBarKeepsTierTitles(t *testing.T) {
	out := ansi.Strip(legendBar([]legendSection{
		{title: "Session", pairs: [][2]string{{"↵", "attach"}}},
		{title: "View", quiet: true, pairs: [][2]string{{"q", "quit"}}},
	}, 200))
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
	out := legendBar([]legendSection{{title: "Session", pairs: pairs}}, 40)
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
