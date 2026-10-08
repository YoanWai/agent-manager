package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFocusEntersOnlyAfterTheProbeRuns(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	if m.mode == modeFocus {
		t.Fatal("focus entered on the update path before the probe")
	}
	if m.effects.main.active == nil {
		t.Fatal("the focus probe should be accepted before the entry")
	}
	m.applyCmd(t, cmd)
	if m.mode != modeFocus {
		t.Fatalf("probe should enter focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

func TestFocusFenceKeepsNewerSelection(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.selectSessionRow(t, "beta")
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("a moved cursor should drop the entry, mode = %v", m.mode)
	}
	if m.errBar.text != "" {
		t.Fatalf("a fenced entry should report nothing, err = %q", m.errBar.text)
	}
}

func TestFocusDeadSessionNeverEnters(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	if m.errBar.text != "" {
		t.Fatalf("the probe reports from its completion, not the update path: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("a dead pane should not enter focus, mode = %v", m.mode)
	}
	if m.errBar.text != deadSessionHint {
		t.Fatalf("err = %q, want %q", m.errBar.text, deadSessionHint)
	}
}

func TestFocusProbeAcknowledgesFinished(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.selectSessionRow(t, "alert-me")
	_, cmd := m.focusSelected()
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished {
		t.Fatalf("the update path acknowledged the session: %q", got.Status)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("after the probe, status = %q acked = %v", got.Status, got.Acked)
	}
	if m.mode != modeFocus {
		t.Fatalf("the probe should enter focus, mode = %v", m.mode)
	}
}

func TestAckDefersTheStoreWrite(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.workspace.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "alert-me")
	_, cmd := m.acknowledgeSelected()
	if cmd == nil {
		t.Fatalf("the acknowledgement should be accepted, err = %q", m.errBar.text)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished {
		t.Fatalf("the update path wrote the store: %q", got.Status)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("after completion, status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestAckIgnoresGoneSession(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "gone", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	result, err := m.captureEffect(ackRequest{sessionID: sess.ID})()
	if err == nil {
		t.Fatal("acknowledging a gone session should report the missing row")
	}
	if _, ok := result.(ackEffectResult); !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if _, lookupErr := m.services.store.Get(sess.ID); lookupErr == nil {
		t.Fatal("the gone session came back")
	}
}

func TestAttachDeadSessionReportsFromCompletion(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "alpha")
	_, cmd := m.attachSelected()
	if m.errBar.text != "" {
		t.Fatalf("the probe reports from its completion, not the update path: %q", m.errBar.text)
	}
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	if m.errBar.text != deadSessionHint {
		t.Fatalf("err = %q, want %q", m.errBar.text, deadSessionHint)
	}
}

func TestAttachWorkerAcknowledgesFinished(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.selectSessionRow(t, "alert-me")
	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished {
		t.Fatalf("the update path acknowledged the session: %q", got.Status)
	}
	work := m.captureEffect(attachRequest{id: sess.ID})
	if _, err := work(); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	got, err = m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("after the worker, status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestAttachWorkerKeepsWorking(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "busy-one", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Working); err != nil {
		t.Fatalf("set working: %v", err)
	}
	m.selectSessionRow(t, "busy-one")
	work := m.captureEffect(attachRequest{id: sess.ID})
	if _, err := work(); err != nil {
		t.Fatalf("attach worker: %v", err)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Working {
		t.Fatalf("after attach, status = %q want %q", got.Status, status.Working)
	}
}

func TestDetachProbeReadsAndClearsInOneStep(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	sess := m.sessionRows()[0]
	clearRequestOnCleanup(t, m)
	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
	if m.effects.main.active == nil {
		t.Fatal("the detach probe should be accepted before the marker is read")
	}
	if request, err := m.services.tmux.PendingRequest(); err != nil || request != tmux.RequestReview {
		t.Fatalf("the marker should be untouched on the update path: %q, %v", request, err)
	}
	m.drainEffects(t)
	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	request, err := m.services.tmux.PendingRequest()
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("opening review should consume the marker once, got %q", request)
	}
}

func TestFocusFenceDropsEntryAfterDialogOpened(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.openForm()
	m.applyCmd(t, cmd)
	if m.mode != modeForm {
		t.Fatalf("a dialog opened after the probe should win, mode = %v", m.mode)
	}
	if m.focus.pane.Pane().SessionID != "" {
		t.Fatalf("focus entered behind the dialog")
	}
}

func TestPasteAndFollowingRawKeyShareTheOrderedLane(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "ordered", t.TempDir(), "")
	m.selectSessionRow(t, "ordered")
	_, cmd := m.focusSelected()
	m.applyCmd(t, cmd)
	delivered := false
	original := pasteFocused
	pasteFocused = func(d *tmux.Driver, id, text string) error { delivered = true; return nil }
	t.Cleanup(func() { pasteFocused = original })
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello"), Paste: true})
	if delivered {
		t.Fatal("paste ran synchronously in key handling")
	}
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if !delivered {
		t.Fatal("accepted paste did not drain before following raw input")
	}
}

func TestFocusCompletionDoesNotReenterAfterNewerInput(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if m.mode == modeFocus {
		t.Fatal("stale focus stole foreground after newer input")
	}
}

func TestDetachCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	request := detachRequest{sessionID: sess.ID}
	m.openForm()
	m.applyDetachEffect(request, detachEffectResult{request: tmux.RequestReview}, nil)
	if m.mode != modeForm {
		t.Fatal("detach completion replaced newer dialog")
	}
}

func TestAcknowledgementCompletionFencesOlderPolls(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions[0].Status = status.Finished
	before := time.Now()
	m.effects.latestObservation = before
	_, cmd := m.acknowledgeSelected()
	m.applyCmd(t, cmd)
	if !m.effects.latestObservation.After(before) {
		t.Fatal("acknowledgment did not fence an older finished-status poll")
	}
}

func TestDetachCompletionDoesNotOpenReviewDuringQuit(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	m.requestQuit()
	m.applyDetachEffect(detachRequest{sessionID: sess.ID}, detachEffectResult{request: tmux.RequestReview}, nil)
	if m.mode != modeList {
		t.Fatal("detach completion opened review during quit")
	}
}

// detachWithReviewRequest leaves the marker Ctrl+R inside alpha leaves and
// detaches, so the probe waits on the lane behind the reflow.
func detachWithReviewRequest(t *testing.T, m *Model) {
	t.Helper()
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	clearRequestOnCleanup(t, m)
	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
}

func TestDetachRequestSurvivesListNavigation(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	detachWithReviewRequest(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if sess, _ := m.selected(); sess.Name != "beta" {
		t.Fatalf("j should move the cursor first, selected %q", sess.Name)
	}
	m.drainEffects(t)
	if m.mode != modeDiff {
		t.Fatalf("a review asked for inside the session was lost to list navigation, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if sess, _ := m.selected(); sess.Name != "alpha" {
		t.Fatalf("review should open for the session that asked, selected %q", sess.Name)
	}
}

func TestDetachRequestDroppedForNewerDialogIsReported(t *testing.T) {
	m := buildModel(t)
	detachWithReviewRequest(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.openForm()
	m.drainEffects(t)
	if m.mode != modeForm {
		t.Fatalf("the newer dialog should keep the foreground, mode = %v", m.mode)
	}
	if !m.errBar.warned() || !strings.Contains(m.errBar.text, "review") || !strings.Contains(m.errBar.text, "alpha") {
		t.Fatalf("a dropped review should say so, status = %q", m.errBar.text)
	}
}

func TestDetachRequestYieldsToQueuedFocus(t *testing.T) {
	m := buildModel(t)
	detachWithReviewRequest(t, m)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.focusSelected()
	m.drainEffects(t)
	if sess, _ := m.selected(); m.mode != modeFocus || sess.Name != "beta" {
		t.Fatalf("the focus asked for after detaching should win, mode = %v, selected %q", m.mode, sess.Name)
	}
	if !m.errBar.warned() || !strings.Contains(m.errBar.text, "review") {
		t.Fatalf("a dropped review should say so, status = %q", m.errBar.text)
	}
}

func TestPendingFocusSurvivesTheInitiatingMouseRelease(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	_, cmd := m.focusSelected()
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease})
	m.applyCmd(t, cmd)
	if m.mode != modeFocus {
		t.Fatal("release of the initiating click cancelled its pending focus")
	}
}
