package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func menuEntry(t *testing.T, m *Model, label string) int {
	t.Helper()
	for i, item := range m.menu.items {
		if item.label == label {
			return i
		}
	}
	t.Fatalf("the menu has no %q entry", label)
	return -1
}

func TestMenuStaysOpenAfterTheDotsRelease(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	frame := strings.Split(ansi.Strip(m.View()), "\n")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	if !strings.Contains(frame[y], "⋯") {
		t.Fatalf("the selected row should paint its menu button: %q", frame[y])
	}
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.railWidth, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.menu.active {
		t.Fatal("a click on ⋯ should open the row menu")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.railWidth, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.menu.active || m.mode != modeList {
		t.Fatalf("the release after ⋯ should leave the menu up and unfocused, menu = %v mode = %v", m.menu.active, m.mode)
	}
}

func TestRightClickOpensTheRowMenuAndRunsAnEntry(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	m = railMouse(t, m, "beta", tea.MouseActionPress, tea.MouseButtonRight)
	if !m.menu.active {
		t.Fatal("right click should open the row menu")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("right click should select its row, got %q", sess.Name)
	}
	frame := ansi.Strip(m.View())
	for _, label := range []string{"Attach", "Rename", "Delete"} {
		if !strings.Contains(frame, label) {
			t.Fatalf("menu should list %s:\n%s", label, frame)
		}
	}
	rename := menuEntry(t, m, "Rename")
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.menu.left + 2, Y: m.menu.top + 1 + rename, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.menu.active || m.mode != modeRename {
		t.Fatalf("clicking Rename should open rename, menu = %v mode = %v", m.menu.active, m.mode)
	}
}

func TestRowMenuClosesOnEscAndOutsideClick(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")

	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.menu.active {
		t.Fatal("esc should close the menu")
	}
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	m.View()
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.width - 1, Y: m.height - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if m.menu.active || m.mode != modeList {
		t.Fatalf("a click outside should close the menu only, menu = %v mode = %v", m.menu.active, m.mode)
	}
}

func TestRightClickOnTheRailWhileFocusedOpensTheMenu(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)

	m = railMouse(t, m, "beta", tea.MouseActionPress, tea.MouseButtonRight)
	if m.mode != modeList || !m.menu.active {
		t.Fatalf("right click on the rail should leave focus and open the menu, mode = %v menu = %v", m.mode, m.menu.active)
	}
}

func TestGroupMenuCreatesAndNeverAttaches(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")
	for i, row := range m.rows {
		if row.isGroup && row.group == "work" {
			m.openRowMenu(i, 2, 2)
		}
	}
	var labels []string
	for _, item := range m.menu.items {
		labels = append(labels, item.label)
	}
	joined := strings.Join(labels, ",")
	if !strings.Contains(joined, "New session") || strings.Contains(joined, "Attach") {
		t.Fatalf("group menu should create and never attach, got %v", labels)
	}
}

func TestDotsOnAnUnselectedRowOpenItsMenuAtOnce(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "beta")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.railWidth, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if sess, ok := m.selected(); !m.menu.active || !ok || sess.Name != "beta" {
		t.Fatalf("⋯ on beta should open beta's menu in one click, menu = %v selected = %q", m.menu.active, sess.Name)
	}
}

func TestMenuPicksTheEntryAPressIsReleasedOn(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.railWidth, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	m.View()
	rename := menuEntry(t, m, "Rename")
	at := tea.MouseMsg{X: m.menu.left + 2, Y: m.menu.top + 1 + rename, Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionMotion
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.menu.index != rename {
		t.Fatalf("dragging over Rename should highlight it, index = %d want %d", m.menu.index, rename)
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
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.railWidth, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.railWidth, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if cmd := m.syncMouseCapture(); cmd == nil || !m.mouseHover {
		t.Fatal("an open menu should ask the terminal for every motion")
	}
	m.View()
	last := len(m.menu.items) - 1
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.menu.left + 2, Y: m.menu.top + 1 + last, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = updated.(*Model)
	if m.menu.index != last || !m.menu.active {
		t.Fatalf("hovering the last entry should highlight it, index = %d want %d", m.menu.index, last)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd := m.syncMouseCapture(); cmd == nil || m.mouseHover {
		t.Fatal("closing the menu should hand motion back to button tracking")
	}
}
