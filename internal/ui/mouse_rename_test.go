package ui

import (
	"strings"
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// frameRow is the screen row whose text starts the detail column with label.
func frameRow(t *testing.T, m *Model, label string) int {
	t.Helper()
	leftWidth, _ := m.splitWidths()
	for y, line := range strings.Split(ansi.Strip(m.viewListFrame()), "\n") {
		if runes := []rune(line); len(runes) > leftWidth && strings.HasPrefix(strings.TrimLeft(string(runes[leftWidth:]), " ▐"), label) {
			return y
		}
	}
	t.Fatalf("no %q row in the detail column:\n%s", label, ansi.Strip(m.viewListFrame()))
	return -1
}

func (m *Model) clickAt(x, y int) {
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
}

// The group edit answers the mouse the way New Group does: a click focuses
// a row, a second click steps it, and the edit saves what was clicked.
func TestGroupEditFieldsAnswerClicks(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	defineEnvs(t, m, "work")
	if err := m.services.store.CreateGroup("team", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "team")
	m.openRename()
	rename, ok := surfaceOf[*uidialogs.RenameSurface](m)
	if !ok {
		t.Fatal("test setup did not open the group edit")
	}
	leftWidth, _ := m.splitWidths()
	x := leftWidth + 6

	y := frameRow(t, m, "env")
	m.clickAt(x, y)
	if rename.Focus() != uidialogs.RenameEnvField || rename.Env != "" {
		t.Fatalf("first click on env = focus %d env %q, want the row focused", rename.Focus(), rename.Env)
	}
	m.clickAt(x, y)
	if rename.Env != "work" {
		t.Fatalf("second click on env = %q, want work", rename.Env)
	}
	y = frameRow(t, m, "worktree")
	m.clickAt(x, y)
	m.clickAt(x, y)
	if rename.Focus() != uidialogs.RenameWorktreeField || rename.Worktree() != 1 {
		t.Fatalf("two clicks on worktree = focus %d index %d", rename.Focus(), rename.Worktree())
	}
	m.clickAt(leftWidth-2, y)
	if rename.Focus() != uidialogs.RenameWorktreeField {
		t.Fatal("a click on the rail moved the edit's focus")
	}
	// A right click, whose release X10 reports without its button, steps
	// nothing.
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	if rename.Worktree() != 1 {
		t.Fatalf("a right click stepped the worktree to %d", rename.Worktree())
	}
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	if rename.Worktree() != 2 {
		t.Fatalf("a left click with a buttonless release left the worktree at %d", rename.Worktree())
	}
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Environment != "work" || groups[0].Worktree != "" {
		t.Fatalf("saved group = %+v, want env work and worktree back to inherit", groups)
	}
}
