package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A notice used to take a row from the body, so the whole layout shrank
// whenever one showed. It floats over the frame now: same rows, same body.
func TestStatusToastKeepsLayoutStill(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		for _, height := range []int{24, 34, 50} {
			quiet := shotModel()
			quiet.width, quiet.height = width, height
			quietRows := strings.Split(quiet.viewListFrame(), "\n")
			quietBody := quiet.listBodyHeight()

			noticed := shotModel()
			noticed.width, noticed.height = width, height
			noticed.errBar.text = "worktree kept (has work): /Users/someone/projects/agent-manager"
			noticedRows := strings.Split(noticed.viewListFrame(), "\n")

			if len(noticedRows) != len(quietRows) {
				t.Errorf("%dx%d: notice changed the frame from %d rows to %d",
					width, height, len(quietRows), len(noticedRows))
			}
			if got := noticed.listBodyHeight(); got != quietBody {
				t.Errorf("%dx%d: notice changed the body from %d rows to %d", width, height, quietBody, got)
			}
			for i, line := range noticedRows {
				if got := ansi.StringWidth(line); got != ansi.StringWidth(quietRows[i]) {
					t.Fatalf("%dx%d: row %d is %d wide with a notice, %d without",
						width, height, i, got, ansi.StringWidth(quietRows[i]))
				}
			}
			if !strings.Contains(ansi.Strip(noticed.viewListFrame()), "worktree kept") {
				t.Errorf("%dx%d: the notice is not on the frame", width, height)
			}
		}
	}
}

// The card sits at the top right, under the header, not over the rail.
func TestStatusToastSitsTopRight(t *testing.T) {
	m := shotModel()
	m.width, m.height = 120, 34
	m.errBar.text = "already up to date"
	rows := strings.Split(ansi.Strip(m.viewListFrame()), "\n")
	row := m.listChromeRows() + 2
	if row >= len(rows) {
		t.Fatalf("frame is only %d rows", len(rows))
	}
	if !strings.Contains(rows[row], "already up to date") {
		t.Fatalf("notice is not on row %d: %q", row, rows[row])
	}
	leftWidth, _ := m.splitWidths()
	if column := strings.Index(rows[row], "already up to date"); column <= leftWidth {
		t.Fatalf("notice starts at column %d, inside the rail (%d wide)", column, leftWidth)
	}
}

// Splicing a card into a styled row must not shift the cells around it.
func TestSpliceAtColumnKeepsWidth(t *testing.T) {
	row := paint(keyStyle.Render("session ")+subtleStyle.Render("running"), 40, panelHex())
	spliced := spliceAtColumn(row, paint(errStyle.Render("boom"), 6, blockHex()), 20)
	if got := ansi.StringWidth(spliced); got != 40 {
		t.Fatalf("spliced row is %d wide, want 40: %q", got, ansi.Strip(spliced))
	}
	if plain := ansi.Strip(spliced); !strings.Contains(plain, "boom") || !strings.HasPrefix(plain, "session ") {
		t.Fatalf("splice lost content: %q", plain)
	}
}

// Search is a field over the list it filters, in the rail, not a card that
// floats away from the entries.
func TestSearchFieldSitsInTheRail(t *testing.T) {
	m := shotModel()
	m.width, m.height = 120, 34
	m.searching = true
	m.search = "rate"

	if status := ansi.Strip(m.statusLine()); strings.Contains(status, "rate") {
		t.Fatalf("the query moved to the rail, the notice should not repeat it: %q", status)
	}
	leftWidth, _ := m.splitWidths()
	rows := strings.Split(ansi.Strip(m.viewListFrame()), "\n")
	found := false
	for _, row := range rows {
		column := strings.Index(row, "⌕ rate")
		if column < 0 {
			continue
		}
		found = true
		if column > leftWidth {
			t.Fatalf("search field starts at column %d, past the rail (%d wide)", column, leftWidth)
		}
	}
	if !found {
		t.Fatalf("no search field on the frame:\n%s", strings.Join(rows, "\n"))
	}
}
