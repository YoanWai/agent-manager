package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The rail's banners (search, archived) cost the list rows, so a short
// terminal has to keep painting entries under them — and keep painting at
// all: claiming rows the list does not have used to slice past the end.
func TestRailBannersSurviveShortTerminals(t *testing.T) {
	for _, height := range []int{4, 6, 8, 10, 14} {
		for _, width := range []int{30, 60, 120} {
			for _, searching := range []bool{false, true} {
				for _, archived := range []bool{false, true} {
					m := shotModel()
					m.width, m.height = width, height
					m.searching, m.showArchived = searching, archived
					m.errBar.text = "worktree kept (has work): /Users/someone/dev/api"
					rows := strings.Split(m.View(), "\n")
					if len(rows) != height {
						t.Errorf("%dx%d search=%v archived=%v: frame is %d rows",
							width, height, searching, archived, len(rows))
					}
				}
			}
		}
	}
}

// A rail with room for its banners still lists entries under them.
func TestRailBannersLeaveRoomForEntries(t *testing.T) {
	m := shotModel()
	m.width, m.height = 120, 34
	m.searching, m.search = true, "rate"
	rail := railLinesText(m.railLines(36, m.listBodyHeight()))
	if !strings.Contains(rail, "⌕ rate") {
		t.Fatalf("no search field in the rail:\n%s", rail)
	}
	if !strings.Contains(rail, "db-migrations") {
		t.Fatalf("search banner crowded the entries out:\n%s", rail)
	}
}

// A rail too short for the padded block keeps the badges themselves; only a
// rail with no room for entries under them drops them altogether.
func TestFilterBadgesSurviveShortRails(t *testing.T) {
	for _, height := range []int{10, 14, 20} {
		m := shotModel()
		m.width, m.height = 120, height
		m.showArchived = true
		rail := ansi.Strip(railLinesText(m.railLines(36, m.listBodyHeight())))
		if !strings.Contains(rail, "ARCHIVED") {
			t.Errorf("height %d dropped the archived badge:\n%s", height, rail)
		}
	}
}

func railLinesText(lines []contentLine) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line.text + "\n")
	}
	return b.String()
}

// The query is the field being typed into: a rail too tight for the padded
// block keeps the bare field, and a query longer than the rail keeps its end,
// where the caret is.
func TestSearchFieldSurvivesTightRails(t *testing.T) {
	for _, height := range []int{14, 20, 34} {
		m := shotModel()
		m.width, m.height = 120, height
		m.searching, m.search = true, "add-rate-limiting-in-the-public-api-handler"
		rail := railLinesText(m.railLines(36, m.listBodyHeight()))
		if !strings.Contains(rail, "⌕") {
			t.Errorf("height %d dropped the search field:\n%s", height, rail)
		}
		if !strings.Contains(rail, "api-handler") {
			t.Errorf("height %d cut the end of the query away:\n%s", height, rail)
		}
		for _, line := range strings.Split(rail, "\n") {
			if got := ansi.StringWidth(ansi.Strip(line)); got > 36 {
				t.Errorf("height %d: rail line is %d wide: %q", height, got, ansi.Strip(line))
			}
		}
	}
}
