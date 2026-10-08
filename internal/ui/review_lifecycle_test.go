package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// Ctrl+R inside a session opens review and remembers the session, so leaving
// review returns to it rather than dropping to the list.
func TestInSessionReviewRemembersOriginAndReattaches(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m)

	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	m.drainEffects(t)
	*m = *updated.(*Model)

	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if m.reviewNav.ret.kind != reviewReturnAttach || m.reviewNav.ret.sessionID != sess.ID {
		t.Fatalf("review origin = %+v, want attach %q", m.reviewNav.ret, sess.ID)
	}

	// esc leaves review; the live origin session re-attaches.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("esc should leave review, mode = %v", m.mode)
	}
	if m.reviewNav.ret != (reviewReturn{}) {
		t.Fatalf("reattach origin should be consumed, got %+v", m.reviewNav.ret)
	}
	if cmd == nil {
		t.Fatal("esc from in-session review should re-attach the session, got nil command")
	}
}

// Review opened from the list has no origin, so esc returns to the list with
// no re-attach.
func TestListReviewLeavesToListWithoutReattach(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "listreview", t.TempDir(), "")
	m.selectSessionRow(t, "listreview")

	m.drainCmds(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if m.reviewNav.ret.kind != reviewReturnList || m.reviewNav.ret.sessionID != "" {
		t.Fatalf("list review should return to list, got %+v", m.reviewNav.ret)
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
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "finisher", t.TempDir(), "")
	m.selectSessionRow(t, "finisher")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m)

	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("expected review, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("esc should re-attach, err = %q", m.errBar.text)
	}
	if foreground := m.foregroundTestCmd(t, cmd); foreground == nil {
		t.Fatal("prepared reattach did not emit terminal command")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("re-attach should acknowledge finished: status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestStaleReattachDoesNotInterruptReopenedReview(t *testing.T) {
	m := &Model{mode: modeDiff}
	seedReviewForTest(m, uireview.Target{ID: "new"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	m.effects.active = &effectJob{id: 1, request: attachRequest{id: "old", reattach: true, generation: 0}}
	updated, cmd := m.Update(effectCompletedMsg{id: 1, result: attachEffectResult{}})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale re-attach should not return an attach command")
	}
	if m.mode != modeDiff || !m.review.Snapshot().Active {
		t.Fatal("stale re-attach should leave the reopened review untouched")
	}
}

func TestReviewCloseReleasesStateAndIgnoresLateLoad(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "close", gitRepoWithTwoChangedFiles(t))
	load := m.switchDiffFile(1)
	if load == nil {
		t.Fatal("switch should leave a file load in flight")
	}

	if cmd := m.closeDiff(); cmd != nil {
		t.Fatal("list-opened review should close without re-attaching")
	}
	if m.mode != modeList || m.review.Snapshot().Active || m.review.Snapshot().SessionID != "" {
		t.Fatalf("review did not close cleanly: mode=%v active=%v session=%q",
			m.mode, m.review.Snapshot().Active, m.review.Snapshot().SessionID)
	}
	if len(m.review.Snapshot().Set.Files) != 0 || m.review.CurrentHighlight() != nil {
		t.Fatal("close should release diff and pending highlight state immediately")
	}

	late := load()
	updated, next := m.Update(late)
	*m = *updated.(*Model)
	if next != nil || len(m.review.Snapshot().Set.Files) != 0 || m.review.Snapshot().Active {
		t.Fatal("a file load landing after close must be ignored")
	}
}

func TestCtrlRFromListOpensReview(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "ctrlr", gitRepoWithTwoChangedFiles(t), "")
	m.selectSessionRow(t, "ctrlr")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("ctrl+r from the list should open review, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	if m.reviewNav.ret.kind != reviewReturnList || m.reviewNav.ret.sessionID != "" {
		t.Fatal("review opened from the list should return to the list, not re-attach")
	}
}
