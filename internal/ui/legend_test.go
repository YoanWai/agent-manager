package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
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

func TestLegendBarReservesRowForNextTier(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 40; i++ {
		pairs = append(pairs, [2]string{"k", "an action"})
	}
	out := ansi.Strip(legendBar([]legendSection{
		{title: "Session", pairs: pairs},
		{title: "View", quiet: true, pairs: [][2]string{{"M", "messages"}}},
	}, 30))
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
		store:    st,
		keys:     keybind.DefaultSession(),
		listKeys: keybind.DefaultList(),
		update:   updateInfo{latest: "v0.41.0", version: "0.40.0"},
	}
	bar := legendBar([]legendSection{waiting.viewLegend()}, 160)
	if !strings.Contains(bar, legendAlertStyle.Render("M unread")) {
		t.Fatalf("waiting messages should use the working color:\n%s", bar)
	}
	idle := &Model{
		store:     st,
		keys:      keybind.DefaultSession(),
		listKeys:  keybind.DefaultList(),
		update:    updateInfo{version: "0.40.0"},
		dismissed: map[string]bool{noticeWelcome: true, noticeArrowStep: true},
	}
	idleBar := ansi.Strip(legendBar([]legendSection{idle.viewLegend()}, 160))
	if strings.Contains(idleBar, "unread") {
		t.Fatalf("an empty panel should leave the messages key off:\n%s", idleBar)
	}
}
