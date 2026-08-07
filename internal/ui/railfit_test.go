package ui

import (
	"strings"
	"testing"
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
					m.errBar.text = "worktree kept (has work): /Users/yoan/dev/spaze/api"
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

func railLinesText(lines []contentLine) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line.text + "\n")
	}
	return b.String()
}
