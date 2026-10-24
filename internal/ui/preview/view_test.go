package preview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Every line of a detail head has to stay inside the content column: a fact
// that overflows tears the panel beside it.
func TestFactRowsFitTheirColumn(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		row := FactRow("dir", PlainValue(strings.Repeat("/very/long/path", 6)), "started 3d", width)
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("width %d: fact row is %d wide: %q", width, got, ansi.Strip(row))
		}
	}
}

// The label is padded to the head's label column, so the values under it line
// up as one column whatever the facts say.
func TestFactRowPadsTheLabelColumn(t *testing.T) {
	row := ansi.Strip(FactRow("dir", PlainValue("/w"), "3d", 40))
	if !strings.HasPrefix(row, "dir    "+"/w") {
		t.Fatalf("label column = %q, want the label padded to %d", row, DetailLabelWidth)
	}
	if !strings.HasSuffix(row, "3d") {
		t.Fatalf("reading is not against the right edge: %q", row)
	}
}

func TestTrimmedValueCutsToItsRoom(t *testing.T) {
	value := TrimmedValue("waiting, working, finished")("working")
	if value != "worki…" {
		t.Fatalf("trimmed value = %q, want it cut to the room it got", value)
	}
}

// Nothing fitting whole means the plainest left keeps whatever readable room
// is left for it.
func TestFitColumnsTrimsThePlainestLeft(t *testing.T) {
	got := ansi.Strip(FitColumns([]string{"a very long name", "name"}, []string{"working · 3m"}, 20))
	if ansi.StringWidth(got) != 20 {
		t.Fatalf("fitted row is %d wide, want the column's 20: %q", ansi.StringWidth(got), got)
	}
	if !strings.Contains(got, "working") {
		t.Fatalf("the reading was dropped instead of the name trimmed: %q", got)
	}
}

func TestFitColumnsKeepsTheRichestPairThatFits(t *testing.T) {
	got := ansi.Strip(FitColumns([]string{"rich name", "name"}, []string{"state", ""}, 24))
	if !strings.Contains(got, "rich name") || !strings.Contains(got, "state") {
		t.Fatalf("fit = %q, want both the rich name and the reading", got)
	}
}

func TestLastActivityFallsBackToCreation(t *testing.T) {
	created := time.Unix(1000, 0)
	if got := LastActivity(created, time.Time{}); !got.Equal(created) {
		t.Fatalf("last activity = %v, want the creation %v", got, created)
	}
	changed := time.Unix(2000, 0)
	if got := LastActivity(created, changed); !got.Equal(changed) {
		t.Fatalf("last activity = %v, want the status change %v", got, changed)
	}
}

// A captured pane is laid out bottom-aligned, with its rows exact cells wide
// and the rows past it left blank so no painted tail frames the agent's last
// line.
func TestRowsFillTheColumnBelowTheCapture(t *testing.T) {
	rows := Rows("one\ntwo\n", 4, 6, -1, nil)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want the column's 4", len(rows))
	}
	if rows[0] != "" || rows[1] != "" {
		t.Fatalf("top padding is not blank: %q", rows[:2])
	}
	if got := ansi.StringWidth(rows[3]); got != 6 {
		t.Fatalf("captured row is %d wide, want 6: %q", got, rows[3])
	}
}

// overlay substitutes the loader's line for a screen row it names.
func TestRowsOverlayReplacesNamedRows(t *testing.T) {
	rows := Rows("one\ntwo", 2, 10, -1, func(row int) string {
		if row == 1 {
			return "loader"
		}
		return ""
	})
	if !strings.Contains(ansi.Strip(rows[1]), "loader") {
		t.Fatalf("overlay lost its row: %q", ansi.Strip(rows[1]))
	}
	if !strings.Contains(ansi.Strip(rows[0]), "one") {
		t.Fatalf("an unnamed row was replaced: %q", ansi.Strip(rows[0]))
	}
}

func TestRowsOfAnEmptyPaneIsNil(t *testing.T) {
	if rows := Rows("", 4, 6, -1, nil); rows != nil {
		t.Fatalf("an empty pane painted %d rows", len(rows))
	}
}
