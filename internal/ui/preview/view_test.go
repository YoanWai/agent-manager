package preview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// A trimmed value is handed only the room left beside the label and the
// reading, so the row never tears the panel beside it.
func TestFactRowsFitTheirColumn(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		row := FactRow(FactLabel("dir"), TrimmedValue(strings.Repeat("/very/long/path", 6)), "started 3d", width)
		if got := ansi.StringWidth(row); got > width {
			t.Fatalf("width %d: fact row is %d wide: %q", width, got, ansi.Strip(row))
		}
	}
}

// The label column pads every label to one width, so the values under it line
// up whatever the facts say.
func TestFactRowPadsTheLabelColumn(t *testing.T) {
	row := ansi.Strip(FactRow(FactLabel("dir"), PlainValue("/w"), "3d", 40))
	if !strings.HasPrefix(row, FactLabel("dir")+"/w") {
		t.Fatalf("label column = %q, want the label padded to %d", row, DetailLabelWidth)
	}
	if !strings.HasSuffix(row, "3d") {
		t.Fatalf("reading is not against the right edge: %q", row)
	}
}

func TestTrimmedValueCutsToItsRoom(t *testing.T) {
	value := TrimmedValue("waiting, working, finished")(6)
	if ansi.StringWidth(value) != 6 || !strings.HasSuffix(value, "…") {
		t.Fatalf("trimmed value = %q, want it cut to the six columns it got", value)
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

func rosterRows() (RosterRow, []RosterRow) {
	title := RosterRow{Name: "agent", Tool: "tool", State: "last activity"}
	return title, []RosterRow{
		{Name: "● alpha", Tool: "claude", State: "working · 2m"},
		{Name: "● beta-longer-name", Tool: "codex", State: "idle · 1h"},
	}
}

// A roster is a table: every tool starts on one column whatever the names
// around it do, and every row fills the width it was handed.
func TestRosterAlignsToolsAndFillsItsWidth(t *testing.T) {
	title, rows := rosterRows()
	lines := Roster(title, rows, 76)
	if len(lines) != 3 {
		t.Fatalf("roster is %d rows, want a head and two agents: %q", len(lines), lines)
	}
	column := -1
	for _, line := range lines[1:] {
		at := strings.Index(line, "claude")
		if at < 0 {
			at = strings.Index(line, "codex")
		}
		if at < 0 {
			t.Fatalf("no tool on roster row %q", line)
		}
		if cell := ansi.StringWidth(line[:at]); cell != column && column != -1 {
			t.Errorf("tool column moved from %d to %d: %q", column, cell, line)
		} else if column == -1 {
			column = ansi.StringWidth(line[:at])
		}
		if got := ansi.StringWidth(line); got != 76 {
			t.Errorf("roster row is %d wide, want the full 76: %q", got, line)
		}
	}
}

// A column too narrow for all three gives up the tool first and the state
// second: names still answer who is in the group.
func TestRosterShedsToolThenState(t *testing.T) {
	title, rows := rosterRows()

	narrow := strings.Join(Roster(title, rows, 30), "\n")
	if strings.Contains(narrow, "claude") {
		t.Errorf("30 columns kept the tool column: %q", narrow)
	}
	if !strings.Contains(narrow, "working") {
		t.Errorf("30 columns dropped the state: %q", narrow)
	}

	narrower := strings.Join(Roster(title, rows, 16), "\n")
	if strings.Contains(narrower, "claude") || strings.Contains(narrower, "working") {
		t.Errorf("16 columns kept a column over the names: %q", narrower)
	}
	if !strings.Contains(narrower, "alpha") {
		t.Errorf("16 columns dropped the names too: %q", narrower)
	}
}
