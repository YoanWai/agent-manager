package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
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

	if _, err := tmuxCmd("set-option", "-g", "@am_review", "1").CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{})
	*m = *updated.(*Model)

	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
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

	m.drainCmds(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
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

// Leaving review back into a session acknowledges a finished alert, matching
// what entering the session from the list does.
func TestReattachAcknowledgesFinished(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "finisher", t.TempDir(), "")
	m.selectSessionRow(t, "finisher")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	t.Cleanup(func() { m.tmux.ClearReviewRequest() })

	if err := m.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if _, err := tmuxCmd("set-option", "-g", "@am_review", "1").CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{})
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("expected review, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("esc should re-attach, err = %q", m.errBar.text)
	}
	prepared, ok := cmd().(reattachPreparedMsg)
	if !ok {
		t.Fatal("re-attach preparation should run in the returned command")
	}
	if prepared.err != nil {
		t.Fatalf("prepare re-attach: %v", prepared.err)
	}
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("re-attach should acknowledge finished: status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestStaleReattachDoesNotInterruptReopenedReview(t *testing.T) {
	m := &Model{mode: modeDiff, diff: diffState{active: true, gen: 9}}
	updated, cmd := m.Update(reattachPreparedMsg{sessID: "old", diffGen: 8})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale re-attach should not return an attach command")
	}
	if m.mode != modeDiff || !m.diff.active {
		t.Fatal("stale re-attach should leave the reopened review untouched")
	}
}
