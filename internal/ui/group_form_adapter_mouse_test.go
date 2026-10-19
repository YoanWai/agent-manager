package ui

import "testing"

func TestClicksOnTheGroupForm(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.openGroupForm()
	x, y := paintedCell(t, m, "worktree ")
	m = leftClick(m, x, y)
	m = leftClick(m, x, y)
	if m.groupForm.Worktree() != 1 {
		t.Fatalf("a second click on worktree should step it, at %d", m.groupForm.Worktree())
	}
	x, y = paintedCell(t, m, "parent ")
	m = leftClick(m, x, y)
	x, y = paintedCell(t, m, "  target")
	if m = leftClick(m, x, y); m.selectedGroupPath() != "target" || m.mode != modeGroupForm {
		t.Fatalf("a click on target in the picker should pick it, picked %q mode %v", m.selectedGroupPath(), m.mode)
	}
	if m = leftClick(m, 0, y); m.mode != modeGroupForm || m.selectedGroupPath() != "target" {
		t.Fatalf("a click beside the card should change nothing, picked %q mode %v", m.selectedGroupPath(), m.mode)
	}
}
