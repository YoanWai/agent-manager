package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

func TestCreateArchiveRestoreDelete(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	createSession(t, m, "alpha", dir, "")
	if len(m.sessionRows()) != 1 {
		t.Fatalf("after create, sessions = %d want 1 (err=%q)", len(m.sessionRows()), m.errBar.text)
	}
	sess := m.sessionRows()[0]
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should exist after create")
	}
	if sess.Name != "alpha" || sess.Tool != "claude" || sess.Group != "" {
		t.Fatalf("session fields wrong: %+v", sess)
	}

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if len(m.sessionRows()) != 0 {
		t.Fatalf("after archive, active sessions = %d want 0", len(m.sessionRows()))
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 || !m.sessionRows()[0].Archived {
		t.Fatalf("archived session should show in archived view")
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	m.showArchived = false
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("after restore, active sessions = %d want 1", len(m.sessionRows()))
	}

	m.selectSessionRow(t, "alpha")
	m.prepareDelete()
	if m.mode != modeConfirmDelete {
		t.Fatal("prepareDelete should enter confirm mode")
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should be killed after delete")
	}
	m.applyCmd(t, cmd)
	if len(m.sessionRows()) != 0 {
		t.Fatalf("after delete, sessions = %d want 0", len(m.sessionRows()))
	}
}

func TestDeleteGroupSubtree(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("zone/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	createSession(t, m, "in-inner", dir, "zone/inner")
	createSession(t, m, "outside", dir, "")

	archivedID := m.sessionRows()[0].ID
	for _, s := range m.sessionRows() {
		if s.Name == "in-inner" {
			archivedID = s.ID
		}
	}
	if err := m.store.SetArchived(archivedID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	for i, r := range m.rows {
		if r.isGroup && r.group == "zone" {
			m.cursor = i
		}
	}
	m.prepareDelete()
	if !m.confirm.isGroup || len(m.confirm.sessions) != 2 {
		t.Fatalf("confirm should target 2 subtree sessions (incl. archived), got %+v", m.confirm)
	}
	tmuxIDs := make([]string, 0, 2)
	for _, s := range m.confirm.sessions {
		tmuxIDs = append(tmuxIDs, s.ID)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	for _, id := range tmuxIDs {
		if m.tmux.Exists(id) {
			t.Fatalf("tmux session %s should be killed", id)
		}
	}
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Name != "outside" {
		t.Fatalf("only outside should remain, got %v", sessions)
	}
	all, _ := m.store.ListSessions(true)
	if len(all) != 1 {
		t.Fatalf("archived subtree session should be gone from db, got %d rows", len(all))
	}
	groups, _ := m.store.Groups()
	for _, g := range groups {
		if g.Name == "zone" || g.Name == "zone/inner" {
			t.Fatalf("group %s should be deleted", g.Name)
		}
	}
}

func TestDeleteGroupInArchivedViewSparesLiveSessions(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("bugs", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "old", dir, "bugs")
	createSession(t, m, "live", dir, "bugs")

	m.selectSessionRow(t, "old")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "bugs")
	m.prepareDelete()
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].Name != "old" {
		t.Fatalf("confirm should target only the archived session, got %+v", m.confirm.sessions)
	}
	archivedID := m.confirm.sessions[0].ID
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.showArchived = false
	m.applyCmd(t, m.refreshCmd())
	if names := sessionNames(m); len(names) != 1 || names[0] != "live" {
		t.Fatalf("active view sessions = %v want [live]", names)
	}
	if paths := m.groupRowPaths(); len(paths) != 1 || paths[0] != "bugs" {
		t.Fatalf("group holding a live session should survive, got %v", paths)
	}
	for _, sess := range m.sessionRows() {
		if !m.tmux.Exists(sess.ID) {
			t.Fatalf("live session %s lost its tmux window", sess.Name)
		}
	}
	if m.tmux.Exists(archivedID) {
		t.Fatalf("archived session %s should be killed", archivedID)
	}
}

func TestDeleteArchivedGroupInArchivedViewRemovesIt(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.prepareDelete()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if paths := m.groupRowPaths(); len(paths) != 0 {
		t.Fatalf("archived empty group should be gone, got %v", paths)
	}
}

func TestIgnoreDeletedSessionDropsOnlyTheDeleteRace(t *testing.T) {
	if err := ignoreDeletedSession(fmt.Errorf("abc: %w", store.ErrSessionGone)); err != nil {
		t.Fatalf("a session deleted mid-poll should not fail the pass: %v", err)
	}
	if err := ignoreDeletedSession(errors.New("database is locked")); err == nil {
		t.Fatal("a real store failure must still surface")
	}
}

func TestAttachDoneOpensReviewWhenMarkerSet(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	t.Cleanup(func() { m.tmux.ClearReviewRequest() })

	if _, err := tmuxCmd("set-option", "-g", "@am_review", "1").CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{})
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	requested, err := m.tmux.ReviewRequested()
	if err != nil {
		t.Fatalf("ReviewRequested: %v", err)
	}
	if requested {
		t.Fatal("opening review should consume the marker")
	}
}

func TestAttachDoneStaysInListWithoutMarker(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "plainexit", t.TempDir(), "")
	m.selectSessionRow(t, "plainexit")
	if err := m.tmux.ClearReviewRequest(); err != nil {
		t.Fatalf("clear marker: %v", err)
	}

	updated, _ := m.Update(attachDoneMsg{})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("no marker should stay in list, mode = %v", m.mode)
	}
}

func TestAttachAcknowledgesFinished(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alert-me", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	m.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "alert-me")

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	got, err := m.store.Get(sess.ID)
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
			if err := m.store.UpdateStatus(sess.ID, tc.stored); err != nil {
				t.Fatalf("set stored status: %v", err)
			}
			m.sessions[0].Status = tc.listed
			m.rebuildRows()
			m.selectSessionRow(t, "alert-me")
			if offered := strings.Contains(m.viewFooter(), "mark idle"); offered != tc.wantOffer {
				t.Fatalf("dot footer offered = %v, want %v", offered, tc.wantOffer)
			}

			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
			got, err := m.store.Get(sess.ID)
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
	if err := m.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if err := m.store.SetArchived(sess.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.sessions[0].Status = status.Finished
	m.sessions[0].Archived = true
	m.showArchived = true
	m.rebuildRows()
	m.selectSessionRow(t, "kept")
	if strings.Contains(m.viewFooter(), "mark idle") {
		t.Fatal("dot offered on an archived session")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	got, err := m.store.Get(sess.ID)
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
	if err := m.store.UpdateStatus(sess.ID, status.Working); err != nil {
		t.Fatalf("set working: %v", err)
	}
	m.sessions[0].Status = status.Working
	m.rebuildRows()
	m.selectSessionRow(t, "busy-one")

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
	got, err := m.store.Get(sess.ID)
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
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.applyCmd(t, cmd)

	sess := m.sessionRows()[0]
	if sess.Tool != "claude-hooked" {
		t.Fatalf("session tool = %q, want claude-hooked", sess.Tool)
	}
	if err := m.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	sess.Status = status.Finished
	m.sessions[0].Status = status.Finished
	m.rebuildRows()
	m.selectSessionRow(t, "attach-reflow")

	before := "final answer line that wraps differently after attach\n❯ \n"
	after := "final answer line that wraps\ndifferently after attach\n❯ \n"
	seedRegionHash(t, m, sess, before)
	// Without clearing, the widened pane looks like streaming work.
	if got := deriveStatus(t, m, sess, after, true); got != status.Working {
		t.Fatalf("reflow with a prior hash should look like working (precondition), got %q", got)
	}

	if _, cmd := m.attachSelected(); cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}

	entered, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := deriveStatus(t, m, entered, after, true); got != status.Idle {
		t.Fatalf("attach must rebaseline the pane hash instead of flashing working, got %q", got)
	}
}

func TestReviveRecreatesDeadSession(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if m.tmux.Exists(sess.ID) {
		t.Fatal("session should be dead before revive")
	}
	m.selectSessionRow(t, "phoenix")

	if err := m.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}

	if _, _ = m.reviveSelected(); m.errBar.text != "" {
		t.Fatalf("revive: %q", m.errBar.text)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("revive should recreate the tmux session")
	}
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle {
		t.Fatalf("after revive, status = %q want %q", got.Status, status.Idle)
	}
	if got.Acked {
		t.Fatal("revive should clear a leftover ack")
	}
}

// argCaptureCommand builds a launch command that records the arguments the
// manager appended to it and then holds the pane open, so a test can prove
// which flags a launch carried.
func argCaptureCommand(argsFile string) string {
	script := `printf '%s\n' "$@" > ` + tmux.ShellQuote(argsFile) + `; cat`
	return "sh -c " + tmux.ShellQuote(script) + " sh"
}

// readWhenWritten waits for content, not merely for the file: the launching
// shell truncates it before printf runs, so a read that lands between the two
// comes back empty and would fail the assertion it was fetched for.
func readWhenWritten(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
			return string(raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no launch arguments written to %s", path)
	return ""
}

// Restart is revive's opposite number: same row, same directory, same tool,
// but a conversation the agent has never seen. The old one is retired rather
// than resumed, so the resume flags revive would have used stay unused.
func TestRestartLaunchesAFreshConversation(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")
	sess := m.sessionRows()[0]

	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.cfg.Tools[sess.Tool]
	tool.Command = argCaptureCommand(argsFile)
	tool.SessionIDFlag = "--session-id"
	tool.ResumeByIDCommand = "false --resume {id}"
	tool.ReviveCommand = "false --continue"
	m.cfg.Tools[sess.Tool] = tool

	if err := m.store.SetAgentSessionID(sess.ID, "old-conversation"); err != nil {
		t.Fatal(err)
	}
	m.sessions[0].AgentSessionID = "old-conversation"
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "phoenix")

	if _, _ = m.restartSelected(); m.mode != modeConfirmDelete {
		t.Fatalf("restart should ask first, mode = %v err = %q", m.mode, m.errBar.text)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("restart should leave the session running")
	}

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID == "" || got.AgentSessionID == "old-conversation" {
		t.Fatalf("restarted conversation id = %q, want a fresh one", got.AgentSessionID)
	}
	if got.RetiredAgentSessionID != "old-conversation" {
		t.Fatalf("retired conversation = %q, want old-conversation", got.RetiredAgentSessionID)
	}
	if got.AgentLaunchedAt.Before(got.CreatedAt) || got.AgentLaunchedAt.IsZero() {
		t.Fatalf("launch time = %v, created = %v", got.AgentLaunchedAt, got.CreatedAt)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restart, status = %q want %q", got.Status, status.Starting)
	}

	// The fresh conversation id rides the launch; the resume flags revive
	// would have used never appear.
	args := readWhenWritten(t, argsFile)
	if !strings.Contains(args, "--session-id\n"+got.AgentSessionID) {
		t.Fatalf("launch arguments = %q, want the fresh session id", args)
	}
	if strings.Contains(args, "--resume") || strings.Contains(args, "--continue") {
		t.Fatalf("restart must not resume, launch arguments = %q", args)
	}
}

// A tool that mints its own conversation id (codex, opencode) has nothing to
// hand the launch, so restart clears the binding and leaves the id for the
// poller to capture once the new conversation lands.
func TestRestartClearsCapturedConversationID(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "codexish", t.TempDir(), "")
	sess := m.sessionRows()[0]
	// The precondition under test, spelled out rather than inherited from
	// whatever flags the fake tools happen to carry.
	tool := m.cfg.Tools[sess.Tool]
	tool.SessionIDFlag = ""
	tool.SessionStore = "codex"
	m.cfg.Tools[sess.Tool] = tool

	if err := m.store.SetAgentSessionID(sess.ID, "captured-conversation"); err != nil {
		t.Fatal(err)
	}
	m.sessions[0].AgentSessionID = "captured-conversation"
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "codexish")

	m.restartSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("conversation id = %q, want it cleared for capture", got.AgentSessionID)
	}
	if got.RetiredAgentSessionID != "captured-conversation" {
		t.Fatalf("retired conversation = %q", got.RetiredAgentSessionID)
	}
}

// Restart also serves the session that is still running: it ends the agent
// holding the context and brings the row back empty-handed.
func TestRestartEndsALiveAgentFirst(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "busy", t.TempDir(), "")
	sess := m.sessionRows()[0]
	tool := m.cfg.Tools[sess.Tool]
	tool.Command = argCaptureCommand(filepath.Join(t.TempDir(), "launch-args"))
	m.cfg.Tools[sess.Tool] = tool
	m.selectSessionRow(t, "busy")

	if _, _ = m.restartSelected(); m.mode != modeConfirmDelete {
		t.Fatalf("restart should ask first, mode = %v err = %q", m.mode, m.errBar.text)
	}
	if !strings.Contains(m.confirm.label, "ends the running agent") {
		t.Fatalf("confirm label = %q", m.confirm.label)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("restart should leave the session running")
	}
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restart, status = %q want %q", got.Status, status.Starting)
	}
}

func TestRestartRefusesGroupRow(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "member", dir, "work")
	m.selectGroupRow(t, "work")

	if _, _ = m.restartSelected(); m.mode != modeList || m.errBar.text == "" {
		t.Fatalf("group restart should refuse, mode = %v err = %q", m.mode, m.errBar.text)
	}
}

func TestNewSessionShowsStartingImmediately(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("boot")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	pickGroup(t, m, "")
	// submitForm without the follow-up refresh: the row must already show the
	// launch state from the optimistic insert alone.
	if _, _ = m.submitForm(); m.errBar.text != "" {
		t.Fatalf("submit: %q", m.errBar.text)
	}
	rows := m.sessionRows()
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Status != status.Starting {
		t.Fatalf("new row status = %q, want %q", rows[0].Status, status.Starting)
	}
	t.Cleanup(func() { m.tmux.Kill(rows[0].ID) })
}

func TestReviveAllRecreatesEveryDeadSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	createSession(t, m, "beta", dir, "")

	for _, sess := range m.visibleSessions() {
		if err := m.tmux.Kill(sess.ID); err != nil {
			t.Fatalf("kill %s: %v", sess.Name, err)
		}
	}
	// A refresh marks the pane-less sessions dead so revive-all picks them up.
	m.applyCmd(t, m.refreshCmd())

	if _, _ = m.reviveAllDead(); m.errBar.text != "" {
		t.Fatalf("revive all: %q", m.errBar.text)
	}
	for _, sess := range m.visibleSessions() {
		if !m.tmux.Exists(sess.ID) {
			t.Fatalf("revive all should recreate %s", sess.Name)
		}
	}
}

func TestReviveRefusesLiveSession(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alive", t.TempDir(), "")
	m.selectSessionRow(t, "alive")

	if _, _ = m.reviveSelected(); m.errBar.text == "" {
		t.Fatal("revive on a live session should error")
	}
	if !m.tmux.Exists(m.sessionRows()[0].ID) {
		t.Fatal("live session must keep running")
	}
}

func TestReviveRefusesMissingDir(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "homeless", dir, "")

	sess := m.sessionRows()[0]
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	m.selectSessionRow(t, "homeless")

	if _, _ = m.reviveSelected(); m.errBar.text == "" {
		t.Fatal("revive without a working directory should error")
	}
}

func TestArchiveRestoreClearStaleError(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")

	m.selectSessionRow(t, "alpha")
	m.errBar.text = "stale failure from an earlier action"
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("archive should clear the stale error, err = %q", m.errBar.text)
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "alpha")
	m.errBar.text = "stale failure from an earlier action"
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restore should clear the stale error, err = %q", m.errBar.text)
	}
}

func TestArchiveAbortsWhenSnapshotFails(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	m.setSnapshot = func(id, snapshot string) error {
		return errors.New("disk full")
	}

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if m.errBar.text != "disk full" {
		t.Fatalf("snapshot failure should surface, err = %q", m.errBar.text)
	}
	if len(m.sessionRows()) != 1 {
		t.Fatalf("failed snapshot must not archive, active sessions = %d want 1", len(m.sessionRows()))
	}
	active, err := m.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 || active[0].Archived {
		t.Fatalf("session should stay unarchived in the store, got %+v", active)
	}
}

func TestArchiveGroupMovesWholeSubtree(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("proj", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.store.CreateGroup("proj/sub", ""); err != nil {
		t.Fatalf("create subgroup: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "top", dir, "proj")
	createSession(t, m, "deep", dir, "proj/sub")

	m.selectGroupRow(t, "proj")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if paths := m.groupRowPaths(); len(paths) != 0 {
		t.Fatalf("active view still shows group rows %v", paths)
	}
	if names := sessionNames(m); len(names) != 0 {
		t.Fatalf("active view still shows sessions %v", names)
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	gotGroups := m.groupRowPaths()
	if len(gotGroups) != 2 || gotGroups[0] != "proj" || gotGroups[1] != "proj/sub" {
		t.Fatalf("archived view groups = %v want [proj proj/sub]", gotGroups)
	}
	if names := sessionNames(m); len(names) != 2 {
		t.Fatalf("archived view sessions = %v want 2", names)
	}

	m.selectGroupRow(t, "proj")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	m.showArchived = false
	m.applyCmd(t, m.refreshCmd())
	if paths := m.groupRowPaths(); len(paths) != 2 {
		t.Fatalf("after restore, active groups = %v want 2", paths)
	}
	if names := sessionNames(m); len(names) != 2 {
		t.Fatalf("after restore, active sessions = %v want 2", names)
	}
}

func TestArchiveGroupKeepsEmptyGroupInArchivedView(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if paths := m.groupRowPaths(); len(paths) != 0 {
		t.Fatalf("archived empty group still in active view: %v", paths)
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if paths := m.groupRowPaths(); len(paths) != 1 || paths[0] != "empty" {
		t.Fatalf("archived view groups = %v want [empty]", paths)
	}
}

// Archiving must freeze the pane as a stored snapshot, and the poller must
// keep serving it instead of wiping the preview on the next tick.
func TestArchivedSessionKeepsPaneSnapshot(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "frozen", t.TempDir(), "")
	m.selectSessionRow(t, "frozen")
	sess := m.sessionRows()[0]

	if err := m.tmux.SendText(sess.ID, "snapshot-marker"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err == nil && strings.Contains(pane, "snapshot-marker") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never showed the marker, last capture: %q", pane)
		}
		time.Sleep(100 * time.Millisecond)
	}

	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	snapshot, err := m.store.Snapshot(sess.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(snapshot, "snapshot-marker") {
		t.Fatalf("archive should persist the pane, snapshot = %q", snapshot)
	}

	m.showArchived = true
	m.applyCmd(t, nil)
	m.selectSessionRow(t, "frozen")
	m.applyCmd(t, nil)
	if !strings.Contains(m.preview, "snapshot-marker") {
		t.Fatalf("archived preview should survive the poll tick, preview = %q", m.preview)
	}

	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.preview = ""
	m.applyCmd(t, nil)
	if !strings.Contains(m.preview, "snapshot-marker") {
		t.Fatalf("archived preview should show the snapshot after tmux is gone, preview = %q", m.preview)
	}

	m.preview = ""
	m.previewGen++
	m.applyCmd(t, m.previewCmd(m.rows[m.cursor].sess, m.previewGen))
	if !strings.Contains(m.preview, "snapshot-marker") {
		t.Fatalf("previewCmd should serve the snapshot for an archived session, preview = %q", m.preview)
	}
}

// waitForPane blocks until a session's pane shows the marker, so a test can
// act on a pane that has actually painted.
func waitForPane(t *testing.T, m *Model, id, marker string) {
	t.Helper()
	if err := m.tmux.SendText(id, marker); err != nil {
		t.Fatalf("send text: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(id)
		if err == nil && strings.Contains(pane, marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never showed %q, last capture: %q", marker, pane)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// confirmKill answers the pending confirm modal with yes.
func confirmKill(t *testing.T, m *Model) {
	t.Helper()
	if m.mode != modeConfirmDelete {
		t.Fatalf("kill should ask before acting, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("kill: %q", m.errBar.text)
	}
}

// seedGroups creates group rows so the new-session picker offers them.
func seedGroups(t *testing.T, m *Model, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := m.store.CreateGroup(path, ""); err != nil {
			t.Fatalf("create group %s: %v", path, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
}

func TestKillEndsTheSessionAndKeepsItRevivable(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "hungry", t.TempDir(), "")
	m.selectSessionRow(t, "hungry")
	sess := m.sessionRows()[0]
	waitForPane(t, m, sess.ID, "kill-marker")

	m.killSelected()
	confirmKill(t, m)

	if m.tmux.Exists(sess.ID) {
		t.Fatal("kill should end the tmux session")
	}
	if len(m.sessionRows()) != 1 {
		t.Fatalf("kill must keep the row, rows = %d", len(m.sessionRows()))
	}
	stored, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != status.Dead {
		t.Fatalf("after kill, status = %q want %q", stored.Status, status.Dead)
	}

	snapshot, err := m.store.Snapshot(sess.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(snapshot, "kill-marker") {
		t.Fatalf("kill should freeze the pane, snapshot = %q", snapshot)
	}
	m.preview = ""
	m.applyCmd(t, nil)
	if !strings.Contains(m.preview, "kill-marker") {
		t.Fatalf("a killed session should still preview its last output, preview = %q", m.preview)
	}

	m.selectSessionRow(t, "hungry")
	if _, _ = m.reviveSelected(); m.errBar.text != "" {
		t.Fatalf("revive after kill: %q", m.errBar.text)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("revive should bring a killed session back")
	}
}

func TestKillGroupEndsEverySessionInside(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work", "work/api")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work/api")
	createSession(t, m, "outside", dir, "")

	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)

	for _, sess := range m.visibleSessions() {
		alive := m.tmux.Exists(sess.ID)
		if sess.Name == "outside" && !alive {
			t.Fatal("a group kill must leave sessions outside the group running")
		}
		if sess.Name != "outside" && alive {
			t.Fatalf("group kill should have ended %s", sess.Name)
		}
	}
}

func TestReviveGroupBringsBackEverySessionInside(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work", "work/api")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work/api")
	createSession(t, m, "outside", dir, "")

	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)

	m.selectGroupRow(t, "work")
	if _, _ = m.reviveSelected(); m.errBar.text != "" {
		t.Fatalf("revive group: %q", m.errBar.text)
	}
	for _, sess := range m.visibleSessions() {
		if !m.tmux.Exists(sess.ID) {
			t.Fatalf("revive group should have brought back %s", sess.Name)
		}
	}
}

func TestKillAllEndsEveryLiveSessionInView(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "outside", dir, "")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = updated.(*Model)
	confirmKill(t, m)

	for _, sess := range m.visibleSessions() {
		if m.tmux.Exists(sess.ID) {
			t.Fatalf("kill all should have ended %s", sess.Name)
		}
	}
	if _, _ = m.killAllLive(); m.errBar.text == "" {
		t.Fatal("kill all with nothing live should report it")
	}
}

func TestKillRefusesWhenNothingIsRunning(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "work")
	createSession(t, m, "ghost", t.TempDir(), "work")
	sess := m.sessionRows()[0]
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}

	m.selectSessionRow(t, "ghost")
	if _, _ = m.killSelected(); m.errBar.text == "" {
		t.Fatal("killing a dead session should report it is already dead")
	}
	if m.mode == modeConfirmDelete {
		t.Fatal("a dead session must not open the kill confirm")
	}

	m.selectGroupRow(t, "work")
	m.errBar.text = ""
	if _, _ = m.killSelected(); m.errBar.text == "" {
		t.Fatal("killing a group with nothing live should report it")
	}
	if m.mode == modeConfirmDelete {
		t.Fatal("a group with nothing live must not open the kill confirm")
	}
}

func deleteSession(t *testing.T, m *Model, name string) {
	t.Helper()
	m.selectSessionRow(t, name)
	m.prepareDelete()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
}

func TestDeleteRemovesCleanWorktree(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.spawnSession("claude", "wt-clean", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, _ := m.store.ListSessions(true)
	worktreePath := sessions[0].Cwd
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "wt-clean")
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("clean worktree should be removed on delete")
	}
}

func TestDeleteKeepsWorktreeUntilLastSharingSession(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	if err := m.spawnSession("claude", "owner", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, err %v", sessions, err)
	}
	owner := sessions[0]
	forked := store.Session{
		ID:             "shared-fork",
		Name:           "forked",
		Tool:           owner.Tool,
		Cwd:            owner.Cwd,
		Status:         status.Idle,
		WorktreeRepo:   owner.WorktreeRepo,
		WorktreeBranch: owner.WorktreeBranch,
	}
	if err := m.tmux.Create(forked.ID, forked.Cwd, "cat", nil, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		t.Fatal(err)
	}
	if err := m.store.CreateSession(forked); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "owner")
	if _, err := os.Stat(owner.Cwd); err != nil {
		t.Fatalf("shared worktree was removed: %v", err)
	}
	if !strings.Contains(m.errBar.text, "used by another session") {
		t.Fatalf("shared worktree message = %q", m.errBar.text)
	}

	m.applyCmd(t, m.refreshCmd())
	deleteSession(t, m, "forked")
	if _, err := os.Stat(owner.Cwd); !os.IsNotExist(err) {
		t.Fatalf("last sharing session left worktree behind: %v", err)
	}
}

func TestDeleteKeepsDirtyWorktree(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.spawnSession("claude", "wt-dirty", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, _ := m.store.ListSessions(true)
	worktreePath := sessions[0].Cwd
	if err := os.WriteFile(filepath.Join(worktreePath, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "wt-dirty")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatal("dirty worktree must survive delete")
	}
	if !strings.Contains(m.errBar.text, worktreePath) {
		t.Fatalf("error bar should name the kept path, got %q", m.errBar.text)
	}
	if remaining, _ := m.store.ListSessions(true); len(remaining) != 0 {
		t.Fatal("session record should still be deleted")
	}
}

// Restart has to hold for every CLI the manager ships with, not just the one
// the fake tools stand in for: it launches each tool the way a brand new
// session does, and never reaches for a resume, continue or fork command.
func TestRestartLaunchIsAFreshStartForEveryShippedTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools) < 6 {
		t.Fatalf("built-in tools = %d, expected every shipped CLI", len(cfg.Tools))
	}
	for name, tool := range cfg.Tools {
		// A shell has no conversation for a restart to start fresh.
		if tool.Shell {
			continue
		}
		command, agentSessionID := restartLaunch(tool)
		if !strings.HasPrefix(command, tool.Command) {
			t.Errorf("%s: restart command %q does not start from its launch command %q", name, command, tool.Command)
		}
		rest := strings.TrimPrefix(command, tool.Command)
		for _, resume := range []string{"resume", "continue", "fork", "--session ", "--last"} {
			if strings.Contains(rest, resume) {
				t.Errorf("%s: restart command %q carries %q", name, command, resume)
			}
		}
		if tool.SessionIDFlag == "" {
			// codex and opencode mint their own id; the poller captures it.
			if agentSessionID != "" || rest != "" {
				t.Errorf("%s: restart handed an id to a tool that mints its own: %q", name, command)
			}
			if tool.SessionStore == "" {
				t.Errorf("%s: no session_id_flag and no session_store leaves restart with no conversation id at all", name)
			}
			continue
		}
		if _, err := uuid.Parse(agentSessionID); err != nil {
			t.Errorf("%s: restart conversation id %q is not a uuid: %v", name, agentSessionID, err)
		}
		if want := " " + tool.SessionIDFlag + " " + agentSessionID; rest != want {
			t.Errorf("%s: restart command tail = %q, want %q", name, rest, want)
		}
		if _, second := restartLaunch(tool); second == agentSessionID {
			t.Errorf("%s: two restarts reused conversation id %q", name, agentSessionID)
		}
	}
}
