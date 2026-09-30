package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"strings"
	"testing"
)

// Every line of a detail head has to stay inside the content column: a fact
// that overflows tears the panel beside it.
func TestDetailHeadsFitTheirColumn(t *testing.T) {
	for _, width := range []int{28, 40, 60, 76, 120} {
		session := shotModel()
		session.workspace.sessions[3].WorktreeBranch = "am/add-rate-limiting"
		session.rail.rows[4].sess.WorktreeBranch = "am/add-rate-limiting"
		session.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}

		group := shotModel()
		for i, row := range group.rail.rows {
			if row.isGroup && row.group == "backend" {
				group.rail.cursor = i
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
	m.workspace.sessions[3].WorktreeBranch = "am/add-rate-limiting"
	m.rail.rows[4].sess.WorktreeBranch = "am/add-rate-limiting"

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

// A group head names the key that spawns into it, since the same key answers
// a session and is read as belonging to sessions alone. The state breakdown
// is the reading on that row, so a column too tight for both keeps it.
func TestGroupHeadNamesTheSpawnKey(t *testing.T) {
	m := shotModel()
	for i, row := range m.rail.rows {
		if row.isGroup && row.group == "backend" {
			m.rail.cursor = i
		}
	}
	wide := ansi.Strip(m.viewDetail(76))
	if !strings.Contains(wide, "space") || !strings.Contains(wide, "new agent") {
		t.Errorf("wide group head does not name the spawn key: %q", wide)
	}
	narrow := ansi.Strip(m.viewDetail(40))
	if strings.Contains(narrow, "new agent") {
		t.Errorf("narrow group head kept the hint over the state: %q", narrow)
	}
	if !strings.Contains(narrow, "working") {
		t.Errorf("narrow group head lost the state breakdown: %q", narrow)
	}
}

// blankCapture is what tmux hands back for a session whose agent has not
// painted yet: one empty row per pane line, not an empty capture.
const blankCapture = "\n\n\n\n\n\n\n\n\n\n"

func previewModel(sessionStatus, preview string) *Model {
	return &Model{
		width: 120, height: 40, mode: modeList, workspace: workspace{preview: preview}, rail: railState{rows: []treeRow{{sess: store.Session{ID: "boot", Name: "boot", Status: sessionStatus}}}},
	}
}

func previewText(m *Model) string {
	var out []string
	for _, line := range m.previewLines(80, 12, "  ") {
		out = append(out, ansi.Strip(line.text))
	}
	return strings.Join(out, "\n")
}

func TestPreviewBottomAlignsCompactPane(t *testing.T) {
	m := previewModel(status.Finished, "todo\ncomposer"+strings.Repeat("\n", 10))
	lines := strings.Split(previewText(m), "\n")
	if strings.TrimSpace(lines[10]) != "todo" || strings.TrimSpace(lines[11]) != "composer" {
		t.Fatalf("compact pane was not bottom aligned: %q", lines)
	}
	wantY := m.listChromeRows() + 10
	if !m.focusPane.pane.box.ok || m.focusPane.pane.box.y != wantY || m.focusPane.pane.box.height != 2 {
		t.Fatalf("pane box = %+v, want two rows starting at %d", m.focusPane.pane.box, wantY)
	}
	if _, _, ok := m.paneCell(m.focusPane.pane.box.x, m.focusPane.pane.box.y-1); ok {
		t.Fatal("padding above a compact pane became hit-testable")
	}
	if row, _, ok := m.paneCell(m.focusPane.pane.box.x, m.focusPane.pane.box.y); !ok || row != 0 {
		t.Fatalf("first compact pane row maps to (%d,%v), want pane row zero", row, ok)
	}
}

// A launching agent paints nothing for a while, and the blank block that
// leaves reads as a broken session; the preview says it is coming up. The
// blank rows are still the session's pane, so they stay hit-testable.
func TestPreviewShowsLoaderWhileSessionStarts(t *testing.T) {
	m := previewModel(status.Starting, blankCapture)
	if got := previewText(m); !strings.Contains(got, "starting up") {
		t.Fatalf("preview should carry the launch loader, got %q", got)
	}
	if !m.focusPane.pane.box.ok || m.focusPane.pane.box.height != len(paneExact(blankCapture, 12, 80, -1)) {
		t.Fatalf("the loader must not cost the pane its geometry, box = %+v", m.focusPane.pane.box)
	}
}

// With no capture at all there are no pane rows to ride, so the loader
// stands in for the empty-preview line.
func TestPreviewShowsLoaderBeforeTheFirstCapture(t *testing.T) {
	m := previewModel(status.Starting, "")
	got := previewText(m)
	if !strings.Contains(got, "starting up") {
		t.Fatalf("preview should carry the launch loader, got %q", got)
	}
	if strings.Contains(got, "(no output yet)") {
		t.Fatalf("a starting session should not read as empty, got %q", got)
	}
	if m.focusPane.pane.box.ok {
		t.Fatalf("no pane rows painted means nothing to hit-test, box = %+v", m.focusPane.pane.box)
	}
}

func TestPreviewLoaderClearsOnFirstFrame(t *testing.T) {
	m := previewModel(status.Starting, "❯ hello\n")
	got := previewText(m)
	if strings.Contains(got, "starting up") {
		t.Fatalf("a captured frame should replace the loader, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("preview should paint the captured frame, got %q", got)
	}
	if !m.focusPane.pane.box.ok {
		t.Fatalf("captured rows must stay hit-testable, box = %+v", m.focusPane.pane.box)
	}
}

// Only the launch state spins: a session that is up with a cleared pane, and
// one that never came up at all, both keep the plain preview.
func TestPreviewSkipsLoaderForSettledSessions(t *testing.T) {
	live := previewModel(status.Idle, blankCapture)
	if got := previewText(live); strings.Contains(got, "starting up") {
		t.Fatalf("an idle session must not spin, got %q", got)
	}
	if !live.focusPane.pane.box.ok {
		t.Fatalf("an idle session keeps its pane rows, box = %+v", live.focusPane.pane.box)
	}
	gone := previewModel(status.Dead, "")
	if got := previewText(gone); !strings.Contains(got, "(no output yet)") {
		t.Fatalf("a session that failed to start should read as empty, got %q", got)
	}
}

func TestPreviewLoaderIsCenteredAndMovesOnThePreviewTick(t *testing.T) {
	m := previewModel(status.Starting, blankCapture)
	first := previewText(m)
	lines := strings.Split(first, "\n")
	painted := make([]int, 0, 6)
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			painted = append(painted, i)
		}
	}
	if len(painted) != 6 || painted[0] != 3 || painted[5] != 8 {
		t.Fatalf("loader rows = %v, want the middle of a 12-row preview", painted)
	}
	if strings.Count(first, "●") != 1 || strings.Count(first, "•") != 1 {
		t.Fatalf("loader should show one head and one trailing dot, got %q", first)
	}
	for _, row := range painted {
		left := len(lines[row]) - len(strings.TrimLeft(lines[row], " "))
		content := strings.TrimSpace(lines[row])
		right := 80 - left - ansi.StringWidth(content)
		if diff := left - right; diff < -1 || diff > 1 {
			t.Fatalf("loader row %q is not centered: left=%d right=%d", lines[row], left, right)
		}
	}
	m.Update(startupTickMsg{})
	if next := previewText(m); next == first {
		t.Fatal("preview loader did not move on the startup tick")
	}
}

// The loader borrows no mark that already names a state: a frame caught
// mid-turn would otherwise read as that state, and the detail head above
// the preview is painting the real one.
func TestPreviewLoaderFramesAreNotStatusMarks(t *testing.T) {
	states := []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Dead, status.Idle}
	for _, frame := range startupFrames {
		for _, state := range states {
			if frame == statusGlyph(state) {
				t.Fatalf("loader frame %q is the %s mark", frame, state)
			}
		}
	}
}

// A focused pane is the screen the user types on, so its first captured row
// keeps the caret drawn there rather than the loader.
func TestPreviewLeavesTheFocusedPaneAlone(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := previewModel(status.Starting, blankCapture)
	m.mode = modeFocus
	m.focusPane.cursorOn = true
	m.focusPane.pane.cursor = paneCursor{ok: true}
	lines := m.previewLines(80, 12, "  ")
	first := lines[m.focusPane.pane.box.y-m.listChromeRows()].text
	if strings.Contains(ansi.Strip(first), "starting up") {
		t.Fatalf("the loader took the focused pane's first row: %q", first)
	}
	if !strings.Contains(first, "\x1b[") {
		t.Fatalf("focused row 0 lost its caret: %q", first)
	}
}
