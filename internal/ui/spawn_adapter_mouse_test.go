package ui

import (
	"path/filepath"
	"slices"
	"testing"

	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

func TestClickOnAPathSuggestionCompletesIt(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	m.form.FocusField(m.spawnHost(), spawn.FieldDir)
	m.form.paths.suggestions = []string{"/work/alpha", "/work/beta"}
	x, y := paintedCell(t, m, "/work/beta")
	m, cmd := leftClickCmd(m, x, y)
	if m.form.Dir() != "/work/beta"+string(filepath.Separator) || cmd == nil {
		t.Fatalf("a click on beta should complete it and read its children, dir %q", m.form.Dir())
	}
}

func TestWheelScrollsTheFormsModelList(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	x, y := paintedCell(t, m, "model ")
	m = leftClick(m, x, y)
	m = leftClick(m, x, y)
	if !m.form.Choice().ListOpen() {
		t.Fatal("test setup: two clicks on the model row open its list")
	}
	m = wheel(m, tea.MouseButtonWheelDown, x, y)
	m = wheel(m, tea.MouseButtonWheelDown, x, y)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.form.Choice().Model(); got != "opus" {
		t.Fatalf("two notches down then enter picked %q, want the second model", got)
	}
}

// The quick bar's model sheet takes the wheel the way it takes ↑↓; the
// session cursor behind the sheet holds still.
func TestWheelScrollsTheQuickModelSheet(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	answered(m, claudeLike, claudeAnswer)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	setQuickTool(m, slices.Index(quickTools(m), "claude"))
	m.quick.RestartChoice(m.spawnHost())
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.quick.Picking() != uiquick.PickModel {
		t.Fatal("test setup: ctrl+l opens the model sheet")
	}
	preparedView(m)
	m = wheel(m, tea.MouseButtonWheelDown, 10, 10)
	m = wheel(m, tea.MouseButtonWheelDown, 10, 10)
	if row, ok := m.selectedRow(); !ok || !row.isGroup || row.group != "work" {
		t.Fatalf("the wheel moved the session cursor behind the sheet to %+v", row)
	}
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.quick.Choice().Model(); got != "haiku" {
		t.Fatalf("two notches down from the first model then enter picked %q, want the third", got)
	}
}
