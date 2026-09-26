package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestHandleLiftsARowStraightFromFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m = liftByHandle(t, m, "beta")
	if m.mode != modeList || m.reorder.key != "s:"+sessionRow(t, m, "beta").sess.ID {
		t.Fatalf("one press on beta's handle should leave focus and lift beta, mode = %v reorder = %+v", m.mode, m.reorder)
	}
}

func liftByHandle(t *testing.T, m *Model, name string) *Model {
	t.Helper()
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, name)[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.handleX[rowKey(m.rows[m.railHits[line]])], Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.reorder.active || !m.reorder.dragging {
		t.Fatalf("a press on the handle should lift %s, reorder = %+v", name, m.reorder)
	}
	return m
}

func sessionOrder(m *Model) []string {
	var names []string
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	return names
}

func TestEveryMovableRowPaintsItsHandle(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	frame := strings.Split(ansi.Strip(m.View()), "\n")
	for _, name := range []string{"alpha", "beta"} {
		y0, _ := m.bodyYRange()
		row := frame[y0+paintedRailLines(t, m, name)[0]]
		if !strings.Contains(row, reorderGrip+" "+name) {
			t.Fatalf("%s should carry the handle right before its name, got %q", name, row)
		}
	}
}

func TestHandleDragReordersAndDrops(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	before := sessionOrder(m)
	m = liftByHandle(t, m, before[0])
	m = railMouse(t, m, before[2], tea.MouseActionMotion, tea.MouseButtonLeft)
	if got := sessionOrder(m); got[2] != before[0] {
		t.Fatalf("dragging onto the last row should move %s there, got %v", before[0], got)
	}
	m = railMouse(t, m, before[0], tea.MouseActionRelease, tea.MouseButtonLeft)
	if m.reorder.active || m.mode != modeList {
		t.Fatalf("release after a drag should drop the row, reorder = %+v mode = %v", m.reorder, m.mode)
	}
}

func TestHandleThenKeysAndEscPutsItBack(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	before := sessionOrder(m)
	m = liftByHandle(t, m, before[0])
	m = railMouse(t, m, before[0], tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.reorder.active || m.mode != modeList {
		t.Fatalf("a release in place keeps the row lifted and unfocused, reorder = %+v mode = %v", m.reorder, m.mode)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := sessionOrder(m); got[2] != before[0] {
		t.Fatalf("down twice should move %s last, got %v", before[0], got)
	}
	m.View()
	if !strings.Contains(ansi.Strip(m.viewFooter()), "Reorder") {
		t.Fatalf("footer should name the reorder mode:\n%s", ansi.Strip(m.viewFooter()))
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := sessionOrder(m); strings.Join(got, ",") != strings.Join(before, ",") || m.reorder.active {
		t.Fatalf("esc should put the row back, got %v want %v", got, before)
	}
}

func TestAnotherHandleTakesOverALiftedRowInOnePress(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	m = railMouse(t, m, "alpha", tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.reorder.active || m.reorder.dragging {
		t.Fatalf("test setup: alpha should stay lifted for the keyboard, reorder = %+v", m.reorder)
	}
	m = liftByHandle(t, m, "beta")
	if m.reorder.key != "s:"+sessionRow(t, m, "beta").sess.ID {
		t.Fatalf("one press on beta's handle should lift beta, reorder = %+v", m.reorder)
	}
}

func TestAPressOnALiftedRowsLabelPutsItDownAndClicks(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	m = railMouse(t, m, "alpha", tea.MouseActionRelease, tea.MouseButtonLeft)
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: m.handleX["s:"+sessionRow(t, m, "alpha").sess.ID] + 4, Y: y0 + paintedRailLines(t, m, "alpha")[0], Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionPress
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.reorder.active || m.mode != modeFocus {
		t.Fatalf("a label click should drop the row and focus it, reorder = %+v mode = %v", m.reorder, m.mode)
	}
}

// Lifting a row selects it, and its preview has to follow: the handle's
// press is the only event before the drag, so its fetch cannot be dropped.
func TestLiftingARowFetchesItsPreview(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.View()
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, "beta")[0]
	_, cmd := m.handleMouse(tea.MouseMsg{X: m.handleX["s:"+sessionRow(t, m, "beta").sess.ID], Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.reorder.active || cmd == nil {
		t.Fatalf("lifting beta should schedule its preview, reorder = %v cmd = %v", m.reorder.active, cmd != nil)
	}
}

// The handle sits on a row's first line only: in a comfortable row the
// prompt and reply lines below it are the row's label, which a click focuses.
func TestHandleColumnBelowTheFirstLineIsTheLabel(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	createSession(t, m, "alpha", t.TempDir(), "")
	m.View()
	y0, _ := m.bodyYRange()
	lines := paintedRailLines(t, m, "alpha")
	if len(lines) < 2 {
		t.Fatalf("test setup: a comfortable row paints more than one line, got %v", lines)
	}
	at := tea.MouseMsg{X: m.handleX[rowKey(sessionRow(t, m, "alpha"))], Y: y0 + lines[1], Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	if m.reorder.active {
		t.Fatal("a press below the first line should not lift the row")
	}
}
