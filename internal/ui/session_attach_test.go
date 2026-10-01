package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestAttachDoneOpensReviewWhenMarkerSet(t *testing.T) {
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
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	request, err := m.services.tmux.PendingRequest()
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("opening review should consume the marker, got %q", request)
	}
}

func TestAttachDoneStaysInListWithoutMarker(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "plainexit", t.TempDir(), "")
	m.selectSessionRow(t, "plainexit")
	if err := m.services.tmux.ClearRequest(); err != nil {
		t.Fatalf("clear marker: %v", err)
	}

	updated, _ := m.Update(attachDoneMsg{})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("no marker should stay in list, mode = %v", m.mode)
	}
}

func TestAttachAcknowledgesFinished(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.workspace.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "alert-me")

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle {
		t.Fatalf("after attach, status = %q want %q", got.Status, status.Idle)
	}
	if !got.Acked {
		t.Fatal("attach should mark the session acked")
	}
}

func TestDotAcknowledgesOnlyCurrentFinishedStatus(t *testing.T) {
	cases := []struct {
		name       string
		listed     string
		stored     string
		wantStatus string
		wantAcked  bool
		wantOffer  bool
	}{
		{name: "finished", listed: status.Finished, stored: status.Finished, wantStatus: status.Idle, wantAcked: true, wantOffer: true},
		{name: "stale snapshot", listed: status.Finished, stored: status.Working, wantStatus: status.Working, wantOffer: true},
		{name: "working", listed: status.Working, stored: status.Working, wantStatus: status.Working},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "alert-me", t.TempDir(), "")
			sess := m.sessionRows()[0]
			if err := m.services.store.UpdateStatus(sess.ID, tc.stored); err != nil {
				t.Fatalf("set stored status: %v", err)
			}
			m.workspace.sessions[0].Status = tc.listed
			m.rebuildRows()
			m.selectSessionRow(t, "alert-me")
			if offered := strings.Contains(m.viewFooter(), "mark idle"); offered != tc.wantOffer {
				t.Fatalf("dot footer offered = %v, want %v", offered, tc.wantOffer)
			}

			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
			m.drainEffects(t)
			got, err := m.services.store.Get(sess.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Status != tc.wantStatus || got.Acked != tc.wantAcked {
				t.Fatalf("after dot, status = %q acked = %v", got.Status, got.Acked)
			}
			if m.mode != modeList {
				t.Fatalf("dot left list mode: %v", m.mode)
			}
		})
	}
}

func TestDotKeepsArchivedFinishedStatus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "kept", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.workspace.sessions[0].Status = status.Finished
	m.workspace.sessions[0].Archived = true
	m.rail.SetArchived(true)
	m.rebuildRows()
	m.selectSessionRow(t, "kept")
	if strings.Contains(m.viewFooter(), "mark idle") {
		t.Fatal("dot offered on an archived session")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	m.drainEffects(t)
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Finished || got.Acked {
		t.Fatalf("archived session changed: status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestAttachKeepsWorking(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "busy-one", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.UpdateStatus(sess.ID, status.Working); err != nil {
		t.Fatalf("set working: %v", err)
	}
	m.workspace.sessions[0].Status = status.Working
	m.rebuildRows()
	m.selectSessionRow(t, "busy-one")

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Working {
		t.Fatalf("after attach, status = %q want %q", got.Status, status.Working)
	}
}

// PrepareAttach flips window-size to auto, which reflows the pane the same
// way the detach-side resize does; without clearing the cached hash first,
// the next poll compares the reflowed pane against a pre-attach hash and
// reads it as working (TestRebaselineKeepsFinishedWithoutFlashingWorking
// proves that precondition). Attach must clear it the same way detach does.
func TestAttachClearsStaleHashBeforeReflow(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("attach-reflow")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 1 // claude-hooked: configured with an activity region to hash
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	sess := m.sessionRows()[0]
	if sess.Tool != "claude-hooked" {
		t.Fatalf("session tool = %q, want claude-hooked", sess.Tool)
	}
	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	sess.Status = status.Finished
	m.workspace.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "attach-reflow")

	if err := m.services.tmux.SendText(sess.ID, "final answer line that wraps differently after attach\n❯ "); err != nil {
		t.Fatal(err)
	}
	resetExecution(m)
	m.applyCmd(t, m.refreshCmd())
	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
	entered, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if entered.Status != status.Idle {
		t.Fatalf("attach must acknowledge and rebaseline the pane, got %q", entered.Status)
	}
}

// y on a dead row names the way back, the way every other lifecycle key
// does, instead of letting the capture fail with raw tmux stderr.
func TestCopyReplyOnADeadSessionHints(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "alpha")
	_, cmd := m.copyReplySelected()
	if cmd == nil {
		t.Fatal("checking the session should run asynchronously")
	}
	if m.errBar.text != "" {
		t.Fatalf("copy reported %q before the command ran", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.errBar.text != deadSessionHint {
		t.Fatalf("errBar = %q, want %q", m.errBar.text, deadSessionHint)
	}
}

// A group has no reply of its own, so y on one does nothing at all.
func TestCopyReplyIgnoresGroupRows(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("team", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "team")
	m.selectGroupRow(t, "team")
	_, cmd := m.copyReplySelected()
	if cmd != nil {
		t.Fatal("a group row should not run a capture")
	}
	if m.errBar.text != "" {
		t.Fatalf("errBar = %q, want no message", m.errBar.text)
	}
}

// The copy reports what landed on the clipboard, and says so plainly when
// the turn held nothing worth copying.
func TestReplyCopiedMsgReports(t *testing.T) {
	m := buildModel(t)
	if _, cmd := m.Update(replyCopiedMsg{name: "alpha"}); cmd != nil {
		t.Fatal("an empty copy should not queue more work")
	}
	if m.errBar.text != "nothing to copy from alpha" {
		t.Fatalf("errBar = %q", m.errBar.text)
	}
	if _, cmd := m.Update(replyCopiedMsg{chars: 42, name: "alpha"}); cmd != nil {
		t.Fatal("a finished copy should not queue more work")
	}
	if m.errBar.text != "copied 42 chars from alpha" || m.errBar.done != m.errBar.text {
		t.Fatalf("errBar = %q done = %q", m.errBar.text, m.errBar.done)
	}
}

// A copy the pane could not bound, and a pane no reply can be read from,
// both name the tool behind the caveat and read as warnings rather than
// as outcomes or failures.
func TestReplyCopiedMsgWarnsOnAnUnboundedCopy(t *testing.T) {
	m := buildModel(t)
	m.Update(replyCopiedMsg{chars: 12426, name: "alpha", tool: "grok", unbounded: true})
	if !strings.Contains(m.errBar.text, "grok marks no turn start here") {
		t.Fatalf("errBar = %q, want the tool named", m.errBar.text)
	}
	if !m.errBar.warned() || m.errBar.worked() {
		t.Fatalf("an unbounded copy should warn, not report success: %+v", m.errBar)
	}

	m = buildModel(t)
	m.Update(replyCopiedMsg{name: "beta", tool: "pi", unreadable: true})
	if !strings.Contains(m.errBar.text, "a pi pane is not read that way") {
		t.Fatalf("errBar = %q, want the tool named", m.errBar.text)
	}
	if !m.errBar.warned() {
		t.Fatalf("an unreadable pane should warn: %+v", m.errBar)
	}
}
