package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every line of a detail head has to stay inside the content column: a fact
// that overflows tears the panel beside it.
func TestDetailHeadsFitTheirColumn(t *testing.T) {
	for _, width := range []int{28, 40, 60, 76, 120} {
		session := shotModel()
		session.sessions[3].WorktreeBranch = "am/add-rate-limiting"
		session.rows[4].sess.WorktreeBranch = "am/add-rate-limiting"
		session.queuedMessages = map[string]int{"add-rate-limiting": 2}

		group := shotModel()
		for i, row := range group.rows {
			if row.isGroup && row.group == "backend" {
				group.cursor = i
			}
		}
		heads := map[string]string{
			"session": session.viewDetail(width),
			"group":   group.viewDetail(width),
			"roster":  group.viewGroupAgents("backend", width, 12),
		}
		for name, head := range heads {
			for i, line := range strings.Split(head, "\n") {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%s head at %d: line %d is %d wide: %q", name, width, i, got, ansi.Strip(line))
				}
			}
		}

		// The head's height feeds previewPaneHeight, so a fourth line would
		// tmux-resize every live pane the moment a message arrived.
		lines := strings.Split(heads["session"], "\n")
		if len(lines) != 3 {
			t.Errorf("badged head at %d is %d lines: %q", width, len(lines), ansi.Strip(heads["session"]))
		}
		if !strings.Contains(ansi.Strip(lines[0]), "✉2") {
			t.Errorf("head at %d dropped the badge: %q", width, ansi.Strip(lines[0]))
		}
	}
}

// The branch chip is the first thing the head gives up, then the tool chip,
// so the name and the state survive the narrowest columns.
func TestDetailHeadShedsChipsBeforeFacts(t *testing.T) {
	m := shotModel()
	m.sessions[3].WorktreeBranch = "am/add-rate-limiting"
	m.rows[4].sess.WorktreeBranch = "am/add-rate-limiting"

	wide := ansi.Strip(strings.Split(m.viewDetail(120), "\n")[0])
	for _, want := range []string{"add-rate-limiting", "claude", "am/add-rate-limiting", "working"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("wide head is missing %q: %q", want, wide)
		}
	}
	if strings.Contains(wide, "✉") {
		t.Errorf("head badged a session holding no queued message: %q", wide)
	}
	mid := ansi.Strip(strings.Split(m.viewDetail(60), "\n")[0])
	if strings.Contains(mid, "am/add-rate-limiting") {
		t.Errorf("60 columns kept the branch chip: %q", mid)
	}
	narrow := ansi.Strip(strings.Split(m.viewDetail(30), "\n")[0])
	if !strings.Contains(narrow, "add-rate") || !strings.Contains(narrow, "working") {
		t.Errorf("30 columns dropped a fact instead of trimming the name: %q", narrow)
	}
}

// A roster is a table: every tool starts on one column and every state ends
// on one edge, whatever the names around them do.
func TestGroupRosterColumnsAlign(t *testing.T) {
	m := shotModel()
	rows := strings.Split(ansi.Strip(m.viewGroupAgents("backend", 76, 12)), "\n")
	if len(rows) < 3 {
		t.Fatalf("roster is only %d rows: %q", len(rows), rows)
	}
	column := -1
	for _, row := range rows[1:] {
		at := strings.Index(row, "claude")
		if at < 0 {
			at = strings.Index(row, "codex")
		}
		if at < 0 {
			t.Fatalf("no tool on roster row %q", row)
		}
		// Byte offsets shift with every multi-byte glyph in a name, so the
		// column is the display width of what precedes the tool.
		cell := ansi.StringWidth(row[:at])
		if column == -1 {
			column = cell
		} else if cell != column {
			t.Errorf("tool column moved from %d to %d: %q", column, cell, row)
		}
		if got := ansi.StringWidth(row); got != 76 {
			t.Errorf("roster row is %d wide, want the full 76: %q", got, row)
		}
	}
}
