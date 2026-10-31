package preview

import (
	"time"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

// DetailLabelWidth is the column every fact label in the content head is
// padded to, so the values under it line up as one column.
const DetailLabelWidth = 7

func FactLabel(label string) string {
	return presentation.PadRight(label, DetailLabelWidth)
}

// FactRow is one line of a detail head: a quiet label, its value, and a
// reading set against the right edge. value is handed the columns it
// actually gets, and right is dropped rather than pushed off the edge when
// the column is too narrow to hold both.
func FactRow(label string, value func(room int) string, right string, width int) string {
	room := width - DetailLabelWidth - ansi.StringWidth(right) - 2
	if room < 12 {
		right, room = "", width-DetailLabelWidth
	}
	return uiframe.RowColumns(label+value(max(room, 1)), right, width)
}

// PlainValue is a FactRow value already short enough to stand as it is, for
// facts whose text does not vary with the terminal.
func PlainValue(value string) func(int) string {
	return func(int) string { return value }
}

// TrimmedValue is a FactRow value cut to the columns it gets, for readings
// that grow with the fleet rather than with the terminal.
func TrimmedValue(value string) func(room int) string {
	return func(room int) string { return ansi.Truncate(value, room, "…") }
}

// FitColumns lays the richest pair of readings that fits: rights are tried
// from richest to plainest, and for each, the lefts in turn. Nothing fitting
// means the plainest left is trimmed to what the column has.
func FitColumns(lefts, rights []string, width int) string {
	for _, right := range rights {
		for _, left := range lefts {
			if ansi.StringWidth(left)+ansi.StringWidth(right)+2 <= width {
				return uiframe.RowColumns(left, right, width)
			}
		}
	}
	last := lefts[len(lefts)-1]
	for _, right := range rights {
		if room := width - ansi.StringWidth(right) - 2; room >= 8 {
			return uiframe.RowColumns(ansi.Truncate(last, room, "…"), right, width)
		}
	}
	return uiframe.RowColumns(ansi.Truncate(last, max(width, 1), "…"), "", width)
}

// LastActivity is when a session last changed state: the agent answering,
// finishing, erroring, or the moment a prompt set it working. statusAt is
// that change, zero when nothing has changed yet and the session still dates
// from its creation. It is what "how long since anything happened here"
// means to someone scanning the rail, where uptime says nothing about
// whether an agent is stuck.
func LastActivity(created, statusAt time.Time) time.Time {
	if statusAt.IsZero() {
		return created
	}
	return statusAt
}

// Rows lays a captured pane into the column: the window Exact picks, worked
// out to exact cells wide, with overlay's line standing in for a screen row
// it names. Rows past the capture are filled in as blank so the column is as
// tall as it was asked for.
func Rows(pane string, height, width, caretRow int, overlay func(row int) string) []string {
	lines := Exact(pane, height, width, caretRow)
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, height)
	top := height - len(lines)
	for len(out) < top {
		out = append(out, "")
	}
	for i, line := range lines {
		if overlay != nil {
			if s := overlay(top + i); s != "" {
				line = s
			}
		}
		out = append(out, Line(line, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// rosterToolColumn is the column a roster's tool names start at, so the roster
// reads as a table rather than as ragged pairs, and rosterNameMin the width
// under which a name column stops being worth reading.
const (
	rosterToolColumn = 14
	rosterNameMin    = 8
)

// RosterRow is one line of a roster: a name, the CLI running it, and what it is
// doing, all already painted.
type RosterRow struct {
	Name  string
	Tool  string
	State string
}

// Roster lays rows out as a table under head, so tools land on one column and
// states share a right edge. The name column is as wide as the longest name
// allows, bounded by what the tool and state columns need. A column too narrow
// for all three gives up the tool first and the state second: a roster of names
// still answers "who is in this group". A name trimmed to the column exactly
// would touch the tool beside it, so the trim leaves the column's last cell as
// the gap.
func Roster(head RosterRow, rows []RosterRow, width int) []string {
	nameWidth, toolWidth, stateWidth := rosterToolColumn, 0, 0
	for _, row := range rows {
		if w := ansi.StringWidth(row.Name) + 2; w > nameWidth {
			nameWidth = w
		}
		if w := ansi.StringWidth(row.Tool); w > toolWidth {
			toolWidth = w
		}
		if w := ansi.StringWidth(row.State); w > stateWidth {
			stateWidth = w
		}
	}
	showTool, showState := true, true
	if width-toolWidth-stateWidth-3 < rosterNameMin {
		showTool, toolWidth = false, 0
	}
	if width-stateWidth-2 < rosterNameMin {
		showState, stateWidth = false, 0
	}
	if room := width - toolWidth - stateWidth - 3; nameWidth > room {
		nameWidth = max(room, rosterNameMin)
	}

	titles := RosterRow{Name: presentation.PadRight(head.Name, nameWidth), Tool: head.Tool, State: head.State}
	lines := []string{rosterLine(titles, width, showTool, showState)}
	for _, row := range rows {
		cells := RosterRow{
			Name:  presentation.PadRight(ansi.Truncate(row.Name, max(nameWidth-1, 1), "…"), nameWidth),
			Tool:  row.Tool,
			State: row.State,
		}
		lines = append(lines, rosterLine(cells, width, showTool, showState))
	}
	return lines
}

func rosterLine(row RosterRow, width int, showTool, showState bool) string {
	line := row.Name
	if showTool {
		line += row.Tool
	}
	state := row.State
	if !showState {
		state = ""
	}
	return uiframe.RowColumns(line, state, width)
}
