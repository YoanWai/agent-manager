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
}

func TestFooterTierFollowsTheCursor(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "legend", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())

	for i, row := range m.rows {
		if !row.isGroup {
			m.cursor = i
			break
		}
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "Session") {
		t.Fatalf("a session under the cursor should title the tier Session:\n%s", footer)
	}

	for i, row := range m.rows {
		if row.isGroup {
			m.cursor = i
			break
		}
	}
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "Group") {
		t.Fatalf("a group under the cursor should title the tier Group:\n%s", footer)
	}
	if strings.Contains(footer, "fork") {
		t.Fatalf("a group cannot be forked, so the key should not be offered:\n%s", footer)
	}
}
