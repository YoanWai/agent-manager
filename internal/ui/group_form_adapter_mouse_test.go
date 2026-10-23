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
	if m.groupSurface().form.Worktree() != 1 {
		t.Fatalf("a second click on worktree should step it, at %d", m.groupSurface().form.Worktree())
	}
	x, y = paintedCell(t, m, "parent ")
	m = leftClick(m, x, y)
	x, y = paintedCell(t, m, "  target")
	if m = leftClick(m, x, y); m.groupSurface().selected() != "target" || !m.groupFormOpen() {
		t.Fatalf("a click on target in the picker should pick it, picked %q mode %v", m.groupSurface().selected(), m.screenID())
	}
	if m = leftClick(m, 0, y); !m.groupFormOpen() || m.groupSurface().selected() != "target" {
		t.Fatalf("a click beside the card should change nothing, picked %q mode %v", m.groupSurface().selected(), m.screenID())
	}
}
