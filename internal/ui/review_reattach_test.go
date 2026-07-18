package ui

import (
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Ctrl+R inside a session opens review and remembers the session, so leaving
// review returns to it rather than dropping to the list.
func TestInSessionReviewRemembersOriginAndReattaches(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	t.Cleanup(func() { m.tmux.ClearReviewRequest() })

	if _, err := exec.Command("tmux", "set-option", "-g", "@am_review", "1").CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{})
	*m = *updated.(*Model)

	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.err)
	}
	if m.diff.reattachID != sess.ID {
		t.Fatalf("review origin = %q, want %q", m.diff.reattachID, sess.ID)
	}

	// esc leaves review; the live origin session re-attaches.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("esc should leave review, mode = %v", m.mode)
	}
	if m.diff.reattachID != "" {
		t.Fatalf("reattach origin should be consumed, got %q", m.diff.reattachID)
	}
	if cmd == nil {
		t.Fatal("esc from in-session review should re-attach the session, got nil command")
	}
}

// Review opened from the list has no origin, so esc returns to the list with
// no re-attach.
func TestListReviewLeavesToListWithoutReattach(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "listreview", t.TempDir(), "")
	m.selectSessionRow(t, "listreview")

	m.applyCmd(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, mode = %v, err = %q", m.mode, m.err)
	}
	if m.diff.reattachID != "" {
		t.Fatalf("list review should not set a reattach origin, got %q", m.diff.reattachID)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("esc should return to list, mode = %v", m.mode)
	}
	if cmd != nil {
		t.Fatal("list review esc should not re-attach")
	}
}
