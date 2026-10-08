package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func menuEntry(t *testing.T, m *Model, label string) int {
	t.Helper()
	preparedView(m)
	for line := 1; line+1 < len(m.layout.displayedRail.Menu); line++ {
		if strings.Contains(ansi.Strip(m.layout.displayedRail.Menu[line]), label) {
			return line - 1
		}
	}
	t.Fatalf("the menu has no %q entry", label)
	return -1
}

func TestMenuStaysOpenAfterTheDotsRelease(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	frame := strings.Split(ansi.Strip(preparedView(m)), "\n")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	if !strings.Contains(frame[y], "[…]") {
		t.Fatalf("the selected row should paint its menu button: %q", frame[y])
	}
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.rail.MenuOpen() {
		t.Fatal("a click on […] should open the row menu")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.rail.MenuOpen() || m.mode != modeList {
		t.Fatalf("the release after […] should leave the menu up and unfocused, menu = %v mode = %v", m.rail.MenuOpen(), m.mode)
	}
}

func TestRightClickOpensTheRowMenuAndRunsAnEntry(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	m = railMouse(t, m, "beta", tea.MouseActionPress, tea.MouseButtonRight)
	if !m.rail.MenuOpen() {
		t.Fatal("right click should open the row menu")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("right click should select its row, got %q", sess.Name)
	}
	frame := ansi.Strip(preparedView(m))
	for _, label := range []string{"Attach", "Rename", "Delete"} {
		if !strings.Contains(frame, label) {
			t.Fatalf("menu should list %s:\n%s", label, frame)
		}
	}
	rename := menuEntry(t, m, "Rename")
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + rename, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.rail.MenuOpen() || m.mode != modeRename {
		t.Fatalf("clicking Rename should open rename, menu = %v mode = %v", m.rail.MenuOpen(), m.mode)
	}
}

func TestRowMenuClosesOnEscAndOutsideClick(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")

	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.MenuOpen() {
		t.Fatal("esc should close the menu")
	}
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	preparedView(m)
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.width - 1, Y: m.layout.height - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if m.rail.MenuOpen() || m.mode != modeList {
		t.Fatalf("a click outside should close the menu only, menu = %v mode = %v", m.rail.MenuOpen(), m.mode)
	}
}

func TestRightClickOnTheRailWhileFocusedOpensTheMenu(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)

	m = railMouse(t, m, "beta", tea.MouseActionPress, tea.MouseButtonRight)
	if m.mode != modeList || !m.rail.MenuOpen() {
		t.Fatalf("right click on the rail should leave focus and open the menu, mode = %v menu = %v", m.mode, m.rail.MenuOpen())
	}
}

func TestGroupMenuCreatesAndNeverAttaches(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")
	m.selectGroupRow(t, "work")
	m = railMouseAtLine(t, m, paintedGroupLines(t, m, "work")[0], tea.MouseActionPress, tea.MouseButtonRight)
	preparedView(m)
	joined := ansi.Strip(strings.Join(m.layout.displayedRail.Menu, "\n"))
	if !strings.Contains(joined, "New session") {
		t.Fatalf("group menu should offer creation:\n%s", joined)
	}
	if !strings.Contains(joined, "Quick prompt mode") || strings.Contains(joined, "Attach") {
		t.Fatalf("group menu should create and never attach:\n%s", joined)
	}
	quick := menuEntry(t, m, "Quick prompt mode")
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + quick, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if !m.quick.active || m.mode != modeList {
		t.Fatalf("group menu should open quick prompt mode, active = %v mode = %v", m.quick.active, m.mode)
	}
}

func TestGroupMenuReviveAsksLikeTheKey(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work")
	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)
	m = railMouseAtLine(t, m, paintedGroupLines(t, m, "work")[0], tea.MouseActionPress, tea.MouseButtonRight)
	revive := menuEntry(t, m, "Revive")
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + revive, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if m.mode != modeConfirmDelete || !m.confirm.isGroup {
		t.Fatalf("Revive on a group with two dead sessions should open the group card, mode = %v isGroup = %v", m.mode, m.confirm.isGroup)
	}
	for _, sess := range m.workspace.sessions {
		if m.services.tmux.Exists(sess.ID) {
			t.Fatalf("the menu revived %s before the question was answered", sess.Name)
		}
	}
}

func TestDotsOnAnUnselectedRowOpenItsMenuAtOnce(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "beta")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if sess, ok := m.selected(); !m.rail.MenuOpen() || !ok || sess.Name != "beta" {
		t.Fatalf("[…] on beta should open beta's menu in one click, menu = %v selected = %q", m.rail.MenuOpen(), sess.Name)
	}
}

func TestMenuPicksTheEntryAPressIsReleasedOn(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	preparedView(m)
	rename := menuEntry(t, m, "Rename")
	at := tea.MouseMsg{X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + rename, Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionMotion
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if !m.rail.MenuOpen() {
		t.Fatal("dragging over Rename closed the menu")
	}
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeRename {
		t.Fatalf("releasing on Rename should run it, mode = %v", m.mode)
	}
}

func TestMenuHighlightsTheEntryUnderAHover(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if cmd := m.syncMouseCapture(); cmd == nil || !m.mouse.hover {
		t.Fatal("an open menu should ask the terminal for every motion")
	}
	preparedView(m)
	last := len(m.layout.displayedRail.Menu) - 3
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + last, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = updated.(*Model)
	if !m.rail.MenuOpen() {
		t.Fatal("hovering the last entry closed the menu")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd := m.syncMouseCapture(); cmd == nil || m.mouse.hover {
		t.Fatal("closing the menu should hand motion back to button tracking")
	}
}

func railMouseAtLine(t *testing.T, m *Model, line int, action tea.MouseAction, button tea.MouseButton) *Model {
	t.Helper()
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + line, Action: action, Button: button})
	return updated.(*Model)
}

// The pending end is reachable both ways: the menu entry for the mouse,
// the footer naming its key for the keyboard.
func TestAPendingEndOffersItsCancelInTheMenuAndTheFooter(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	askAfterTurn(t, m, "alpha", store.AfterTurnArchive)
	loadStoredRows(t, m)
	m.selectSessionRow(t, "alpha")

	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"alpha ↓", "archives when this turn ends", "c cancel archive"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the frame does not show %q:\n%s", want, frame)
		}
	}
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	cancel := menuEntry(t, m, "Cancel archive")
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + cancel, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	sess, _ := m.selected()
	if got, _ := m.services.store.Get(sess.ID); got.AfterTurn != "" {
		t.Fatalf("Cancel archive left %q pending", got.AfterTurn)
	}
	if strings.Contains(ansi.Strip(preparedView(m)), "alpha ↓") {
		t.Fatal("the row still wears the archive mark after the cancel")
	}
}

// A row the poll has not yet ended still carries its request once dead, and
// c cancels it there, so the menu has to as well.
func TestADeadRowWithAPendingEndOffersItsCancel(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)
	if err := m.services.store.UpdateStatus(sess.ID, status.Dead); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	loadStoredRows(t, m)
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	cancel := menuEntry(t, m, "Cancel kill")
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + cancel, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, _ := m.services.store.Get(sess.ID); got.AfterTurn != "" {
		t.Fatalf("Cancel kill on the dead row left %q pending", got.AfterTurn)
	}
}
