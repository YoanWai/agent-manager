package ui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

// The docked bar is bounded by the full screen frame, so a prompt taller
// than the rows it can spare still shows the row the caret is on.
func TestFullQuickLinesKeepTheCaretRowOnScreen(t *testing.T) {
	m := buildModel(t)
	seedTwoGroups(t, m)
	setRailCursor(m, 1)
	m.prefs.fullLayout = true
	m.width = 56
	m.height = 9
	m.openQuickMode()

	body := m.listBodyHeight()
	painted := func() string {
		var out strings.Builder
		for _, line := range m.fullQuickLines(m.width-1, body) {
			out.WriteString(ansi.Strip(line.text) + "\n")
		}
		return out.String()
	}
	painted()
	m = applyMsg(t, m, pasteTextMsg{
		target: composerQuick,
		gen:    m.quick.gen,
		inner: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(
			"FIRSTROWMARK " + strings.Repeat("filler word ", 20) + "LASTROWMARK")},
	})
	if rows := m.fullQuickLines(m.width-1, body); len(rows) > body {
		t.Fatalf("the quick bar painted %d rows into a %d row frame", len(rows), body)
	}

	for i := 0; i < 40 && !m.quick.caretOnFirstRow(); i++ {
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
		painted()
	}
	if !m.quick.caretOnFirstRow() {
		t.Fatal("up never reached the prompt's first row")
	}
	if !strings.Contains(painted(), "FIRSTROWMARK") {
		t.Fatal("the frame clipped the prompt's first row")
	}

	for i := 0; i < 40 && !m.quick.caretOnLastRow(); i++ {
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
		painted()
	}
	if !m.quick.caretOnLastRow() {
		t.Fatal("down never reached the prompt's last row")
	}
	if !strings.Contains(painted(), "LASTROWMARK") {
		t.Fatal("the frame clipped the prompt's last row")
	}
}

func TestQuickBarMeasuresRowsAtTheWidthItJustSet(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetWidth(80)
	m.quick.input.SetValue("one two three four five six seven eight nine ten")
	m.viewQuickBar(14, quickBarMaxRows)
	if m.quick.maxRows < 2 {
		t.Fatalf("rows = %d, want the wrap at width 14, not the previous width", m.quick.maxRows)
	}
}

// The quick bar takes its rows from the painted preview alone: resizing the
// pane for it would make an agent drawing on the normal screen redraw its
// whole transcript, so the pane stays pinned and the view crops instead.
func TestQuickBarKeepsPaneHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sizer", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	sess := railSelectedSession(m)
	pinned := m.previewPaneHeight()

	rows := make([]string, pinned+10)
	for i := range rows {
		rows[i] = fmt.Sprintf("line%03d", i+1)
	}
	m.workspace.preview = strings.Join(rows, "\n") + "\n"
	listed := paintedPreview(m)

	m.openQuickMode()
	if got := m.previewPaneHeight(); got != pinned {
		t.Fatalf("box height with the quick bar open = %d, want %d", got, pinned)
	}
	painted := paintedPreview(m)
	// The bar's rows come off the top of the view; the pane's live end stays.
	if oldest := rows[len(rows)-pinned]; !strings.Contains(listed, oldest) || strings.Contains(painted, oldest) {
		t.Fatalf("the quick bar should have cropped %s off the top:\n%s", oldest, painted)
	}
	if newest := rows[len(rows)-1]; !strings.Contains(painted, newest) {
		t.Fatalf("the quick bar hid the pane's live end (%s):\n%s", newest, painted)
	}
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != pinned {
		t.Fatalf("pane height with the quick bar open = %d, want %d", got, pinned)
	}

	m.quick.active = false
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != pinned {
		t.Fatalf("pane height after closing the quick bar = %d, want %d", got, pinned)
	}
}

func paintedPreview(m *Model) string {
	_, rightWidth := m.splitWidths()
	var painted strings.Builder
	for _, row := range m.contentLines(rightWidth, m.listBodyHeight()) {
		painted.WriteString(ansi.Strip(row.text) + "\n")
	}
	return painted.String()
}
