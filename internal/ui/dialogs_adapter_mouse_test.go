package ui

import (
	"testing"

	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// A click on a target moves the picker's cursor there, and a click on the
// target under the cursor moves the session, the way ↵ does.
func TestClickOnAMoveTargetPicksThenMoves(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	x, y := paintedCell(t, m, "target")
	if m = leftClick(m, x, y); m.mode != modeMove || m.selectedGroupPath() != "target" {
		t.Fatalf("first click should pick the target, mode %v picked %q", m.mode, m.selectedGroupPath())
	}
	if m = wheel(m, tea.MouseButtonWheelUp, x, y); m.selectedGroupPath() != "" {
		t.Fatalf("the wheel should move the cursor back to root, picked %q", m.selectedGroupPath())
	}
	m = leftClick(m, x, y)
	m, cmd := leftClickCmd(m, x, y)
	m.applyCmd(t, cmd)
	m.drainEffects(t)
	if sessions := m.sessionRows(); len(sessions) != 1 || sessions[0].Group != "target" {
		t.Fatalf("the second click should move the session: %+v", sessions)
	}
}

func TestClickAndWheelMoveTheReviewPickersCursor(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	rows := []uireview.PickerRow{{Label: "alpha", Root: "/w/alpha"}, {Label: "beta", Root: "/w/beta"}, {Label: "gamma", Root: "/w/gamma"}}
	m.openReviewPicker(rows, "⌥ Review branch", uireview.PickRepo, "/w/alpha", uireview.PickerSource{}, "")
	x, y := paintedCell(t, m, "beta")
	if m = leftClick(m, x, y); m.reviewNav.picker.Cursor() != 1 || m.mode != modeRepoPick {
		t.Fatalf("a click on beta should move the cursor there, cursor %d mode %v", m.reviewNav.picker.Cursor(), m.mode)
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.reviewNav.picker.Cursor() != 2 {
		t.Fatalf("the wheel should move the cursor on, cursor %d", m.reviewNav.picker.Cursor())
	}
}
