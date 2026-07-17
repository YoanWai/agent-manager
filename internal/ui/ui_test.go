package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func buildModel(t *testing.T) *Model {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	cfg := config.Config{
		Tools: map[string]config.Tool{
			"claude": {Command: "cat", DefaultStatus: status.Idle},
			"claude-hooked": {
				Command:        "cat",
				StatusSource:   "claude-hooks",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
				TurnEnd:        `^[✻✳✶✽✢·✦✧+*] \S+ for \d.*$`,
				Rules: []config.Rule{
					{State: status.Waiting, Pattern: "Enter to confirm"},
					{State: status.Errored, Pattern: `(?im)^\s*error:`},
				},
			},
		},
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	driver, err := tmux.New()
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	m := New(cfg, st, driver, engine, hooks.NewManager(t.TempDir()))
	m.width = 120
	m.height = 40
	t.Cleanup(func() {
		for _, s := range m.sessions {
			driver.Kill(s.ID)
		}
	})
	return m
}

func (m *Model) applyCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		// Actions poke the background poller instead of returning a
		// command; tests run the equivalent refresh synchronously.
		cmd = m.refreshCmd()
	}
	msg := cmd()
	if msg == nil {
		return
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
}

func (m *Model) sessionRows() []store.Session {
	var sessions []store.Session
	for _, r := range m.rows {
		if !r.isGroup {
			sessions = append(sessions, r.sess)
		}
	}
	return sessions
}

func (m *Model) selectSessionRow(t *testing.T, name string) {
	t.Helper()
	for i, r := range m.rows {
		if !r.isGroup && r.sess.Name == name {
			m.cursor = i
			return
		}
	}
	t.Fatalf("no session row named %q", name)
}

func pickGroup(t *testing.T, m *Model, path string) {
	t.Helper()
	for i, opt := range m.form.groups {
		if opt.path == path {
			m.form.groupIndex = i
			return
		}
	}
	t.Fatalf("group %q not in picker options %v", path, m.form.groups)
}

func createSession(t *testing.T, m *Model, name, dir, group string) {
	t.Helper()
	m.openForm()
	m.form.name.SetValue(name)
	m.form.dir.SetValue(dir)
	m.form.toolIndex = 0
	pickGroup(t, m, group)
	_, cmd := m.submitForm()
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.err)
	}
	m.applyCmd(t, cmd)
}

func TestCreateArchiveRestoreDelete(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	createSession(t, m, "alpha", dir, "")
	if len(m.sessionRows()) != 1 {
		t.Fatalf("after create, sessions = %d want 1 (err=%q)", len(m.sessionRows()), m.err)
	}
	sess := m.sessionRows()[0]
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should exist after create")
	}
	if sess.Name != "alpha" || sess.Tool != "claude" || sess.Group != "" {
		t.Fatalf("session fields wrong: %+v", sess)
	}

	m.selectSessionRow(t, "alpha")
	_, cmd := m.archiveSelected()
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
	_, cmd = m.restoreSelected()
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

func TestNestedGroupsTree(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("backend/api/auth", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	createSession(t, m, "deep", dir, "backend/api/auth")
	createSession(t, m, "top", dir, "")

	var groupPaths []string
	for _, r := range m.rows {
		if r.isGroup {
			groupPaths = append(groupPaths, r.group)
		}
	}
	want := []string{"backend", "backend/api", "backend/api/auth"}
	if len(groupPaths) != len(want) {
		t.Fatalf("group rows = %v want %v", groupPaths, want)
	}
	for i := range want {
		if groupPaths[i] != want[i] {
			t.Fatalf("group rows = %v want %v", groupPaths, want)
		}
	}

	if m.rows[0].isGroup || m.rows[0].sess.Name != "top" {
		t.Fatalf("root session should render first, rows[0] = %+v", m.rows[0])
	}

	deep := m.sessionRows()[1]
	if deep.Group != "backend/api/auth" {
		t.Fatalf("deep session group = %q", deep.Group)
	}

	m.collapsed["backend"] = true
	m.rebuildRows()
	if len(m.sessionRows()) != 1 {
		t.Fatalf("collapsing backend should hide the deep session, got %d sessions", len(m.sessionRows()))
	}
	m.collapsed["backend"] = false
	m.rebuildRows()

	m.search = "deep"
	m.rebuildRows()
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Name != "deep" {
		t.Fatalf("search should keep only deep, got %v", sessions)
	}
	m.search = ""
	m.rebuildRows()

	if m.View() == "" {
		t.Fatal("View should render non-empty")
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

func TestRenameGroupCascades(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	for i, r := range m.rows {
		if r.isGroup && r.group == "old" {
			m.cursor = i
		}
	}
	m.collapsed["old"] = true
	m.rebuildRows()
	m.openRename()
	if !m.rename.isGroup || m.rename.path != "old" {
		t.Fatalf("rename target wrong: %+v", m.rename)
	}
	m.rename.input.SetValue("fresh")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	kid := m.sessionRows()
	if len(kid) != 0 {
		t.Fatalf("fresh should stay collapsed after rename, got %d sessions", len(kid))
	}
	if !m.collapsed["fresh"] || m.collapsed["old"] {
		t.Fatalf("collapse state should follow rename: %v", m.collapsed)
	}
	m.collapsed["fresh"] = false
	m.rebuildRows()
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "fresh/inner" {
		t.Fatalf("session group should cascade to fresh/inner, got %+v", sessions)
	}
	groups, _ := m.store.Groups()
	for _, g := range groups {
		if strings.HasPrefix(g.Name, "old") {
			t.Fatalf("old group path survived rename: %v", groups)
		}
	}
}

func TestRenameSession(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	m.openRename()
	m.rename.input.SetValue("after")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.sessionRows()[0].Name != "after" {
		t.Fatalf("rename failed: %+v", m.sessionRows()[0])
	}
}

func TestMoveSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("target/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if m.mode != modeMove {
		t.Fatal("openMove should enter move mode")
	}
	pickGroup(t, m, "target/deep")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "target/deep" {
		t.Fatalf("move failed: %+v", sessions)
	}
}

func TestNewSessionPreselectsContextGroup(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("alpha/beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "seed", dir, "alpha/beta")

	// cursor on the session inside alpha/beta
	m.selectSessionRow(t, "seed")
	m.openForm()
	if got := m.form.groups[m.form.groupIndex].path; got != "alpha/beta" {
		t.Fatalf("form should preselect session's group, got %q", got)
	}
	m.mode = modeList

	// cursor on a group row
	for i, r := range m.rows {
		if r.isGroup && r.group == "alpha" {
			m.cursor = i
		}
	}
	m.openForm()
	if got := m.form.groups[m.form.groupIndex].path; got != "alpha" {
		t.Fatalf("form should preselect the highlighted group, got %q", got)
	}
}

func TestGroupFormCreatesUnderParent(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("projects", ""); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.openGroupForm()
	pickGroup(t, m, "projects")
	m.groupForm.name.SetValue("sub/one")
	m.groupForm.path.SetValue(t.TempDir())
	_, cmd := m.submitGroupForm()
	if m.mode != modeList {
		t.Fatalf("group form should close, err=%q", m.err)
	}
	m.applyCmd(t, cmd)

	groups, _ := m.store.Groups()
	found := ""
	for _, g := range groups {
		if strings.HasSuffix(g.Name, "sub-one") {
			found = g.Name
		}
	}
	if found != "projects/sub-one" {
		t.Fatalf("slash should be sanitized and nested under parent, got %q", found)
	}
}

func TestGroupDefaultPathFillsSessionDir(t *testing.T) {
	m := buildModel(t)
	groupDir := t.TempDir()
	if err := m.store.CreateGroup("workspace", groupDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.openForm()
	pickGroup(t, m, "workspace")
	m.moveGroupCursor(0) // re-resolve dir for the selected group
	if got := m.form.dir.Value(); got != groupDir {
		t.Fatalf("session dir should default to the group path %q, got %q", groupDir, got)
	}
}

func writeHookStatus(t *testing.T, m *Model, id, state string) {
	t.Helper()
	path := m.hooks.StatusFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(state), 0o644); err != nil {
		t.Fatalf("write hook status: %v", err)
	}
}

func deriveStatus(t *testing.T, m *Model, sess store.Session, pane string, agentAlive bool) string {
	t.Helper()
	got, err := m.poller.derivePaneStatus(sess, pane, agentAlive, map[string]uint64{})
	if err != nil {
		t.Fatalf("derivePaneStatus: %v", err)
	}
	return got
}

func TestHookStatusDerivesFinishedAndIdleWhenAcked(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked01", Tool: "claude-hooked"}
	pane := "some output\n❯ \n"
	writeHookStatus(t, m, sess.ID, status.Finished)

	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("hook finished should derive finished, got %q", got)
	}

	sess.Acked = true
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("acked hook finished should derive idle, got %q", got)
	}
}

func TestHookWorkingWinsOverUnmatchedPane(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked02", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "plain streaming text no rule matches\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("hook working should win, got %q", got)
	}
}

func TestHookFinishedUpgradesToWaitingOnQuestionTurn(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked03", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "Do you want me to proceed?\n\n✻ Baked for 5s\n\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("question turn should upgrade hook finished to waiting, got %q", got)
	}
}

func TestHookFinishedUpgradesToErroredOnErrorLine(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked04", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "error: something broke\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Errored {
		t.Fatalf("error line should upgrade hook finished to errored, got %q", got)
	}
}

func TestHookWorkingUpgradesToWaitingOnPaneMatch(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked05", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "Enter to confirm\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("waiting pane verdict should upgrade hook working, got %q", got)
	}
}

func TestStaleHookFileFallsBackToPaneRules(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked06", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "shell prompt after a crash\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, false); got != status.Idle {
		t.Fatalf("dead agent should fall back to pane rules, got %q", got)
	}
	if _, ok := m.hooks.Read(sess.ID); ok {
		t.Fatal("stale hook status file should be removed")
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
		t.Fatalf("attach did not start, err = %q", m.err)
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
		t.Fatalf("attach did not start, err = %q", m.err)
	}
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Working {
		t.Fatalf("after attach, status = %q want %q", got.Status, status.Working)
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

	if _, _ = m.reviveSelected(); m.err != "" {
		t.Fatalf("revive: %q", m.err)
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

func TestReviveRefusesLiveSession(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alive", t.TempDir(), "")
	m.selectSessionRow(t, "alive")

	if _, _ = m.reviveSelected(); m.err == "" {
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

	if _, _ = m.reviveSelected(); m.err == "" {
		t.Fatal("revive without a working directory should error")
	}
}

func TestQuickPromptDeadSessionSetsError(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "gone", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "gone")

	m.openQuickMode()
	m.quick.input.SetValue("hello?")
	if _, _ = m.submitQuick(); m.err != "session is dead - press v to revive" {
		t.Fatalf("err = %q", m.err)
	}
	if !m.quick.active {
		t.Fatal("quick mode should stay open after a failed send")
	}
	if _, err := m.store.Get(sess.ID); err != nil {
		t.Fatalf("session record should survive: %v", err)
	}
}

func TestQuickPromptSendClearsAcked(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.selectSessionRow(t, "answer-me")

	m.openQuickMode()
	if !m.quick.active {
		t.Fatalf("quick mode should activate, err = %q", m.err)
	}
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.err != "" {
		t.Fatalf("send: %q", m.err)
	}
	if !m.quick.active {
		t.Fatal("quick mode should stay active after a send")
	}
	if m.quick.input.Value() != "" {
		t.Fatal("input should clear after a send")
	}
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Acked {
		t.Fatal("quick prompt send should clear the acked flag")
	}
}

func TestQuickSpawnOnGroupCreatesSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.store.SetSetting("default_tool", "claude"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}

	m.openQuickMode()
	m.quick.input.SetValue("build the api")
	_, cmd := m.submitQuick()
	if m.err != "" {
		t.Fatalf("quick spawn: %q", m.err)
	}
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d want 1", len(sessions))
	}
	spawned := sessions[0]
	if spawned.Group != "backend" || spawned.Tool != "claude" || spawned.Cwd != dir {
		t.Fatalf("spawned session fields wrong: %+v", spawned)
	}
	if !m.tmux.Exists(spawned.ID) {
		t.Fatal("tmux session should exist after quick spawn")
	}
	if m.quick.input.Value() != "" {
		t.Fatal("input should clear after a spawn")
	}
}

func TestDefaultToolFallsBackWhenSettingStale(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting("default_tool", "deleted-tool"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if got := m.defaultTool(); got != "claude" {
		t.Fatalf("defaultTool = %q want claude (alphabetical fallback)", got)
	}
}

func TestFormPromptComposesWithSettings(t *testing.T) {
	m := buildModel(t)
	tool := m.cfg.Tools["claude-hooked"]

	command, _, err := m.buildLaunch(tool, withPrompt(tool, tool.Command, "fix the bug"), "prompt01")
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}
	if !strings.HasPrefix(command, "cat 'fix the bug' --settings '") {
		t.Fatalf("command = %q", command)
	}

	flagged := config.Tool{Command: "opencode", PromptFlag: "--prompt"}
	if got := withPrompt(flagged, flagged.Command, "do it"); got != "opencode --prompt 'do it'" {
		t.Fatalf("flagged compose = %q", got)
	}
	if got := withPrompt(tool, tool.Command, ""); got != "cat" {
		t.Fatalf("empty prompt should leave the command untouched, got %q", got)
	}
}

func TestRefreshWithStaleSelectionFetchesPreview(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "fresh-one", t.TempDir(), "")
	m.selectSessionRow(t, "fresh-one")
	sess := m.sessionRows()[0]

	_, cmd := m.Update(refreshMsg{sessions: m.sessions, procFor: ""})
	if cmd == nil {
		t.Fatal("stale refresh should schedule an immediate preview fetch")
	}
	if m.poller.selectedID != sess.ID {
		t.Fatalf("poller selectedID = %q want %q", m.poller.selectedID, sess.ID)
	}

	m.preview = "existing"
	if _, cmd := m.Update(refreshMsg{sessions: m.sessions, procFor: sess.ID, preview: "pane text"}); cmd != nil {
		t.Fatal("matching refresh should not schedule extra work")
	}
	if m.preview != "pane text" {
		t.Fatalf("preview = %q want %q", m.preview, "pane text")
	}
}

func TestFormRejectsDashLeadingPrompt(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("flagged")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	m.form.prompt.SetValue("--version")

	if _, _ = m.submitForm(); m.err == "" {
		t.Fatal("dash-leading prompt should be rejected")
	}
	if m.mode != modeForm {
		t.Fatalf("form should stay open, mode = %v", m.mode)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session should be created, got %d", len(m.sessionRows()))
	}
}

func TestQuickSpawnUsesTabCycledTool(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}

	m.openQuickMode()
	if m.quickTool() != "claude" {
		t.Fatalf("quick tool starts at %q want claude", m.quickTool())
	}
	if _, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyTab}); m.quickTool() != "claude-hooked" {
		t.Fatalf("after tab, quick tool = %q want claude-hooked", m.quickTool())
	}

	m.quick.input.SetValue("build the api")
	_, cmd := m.submitQuick()
	if m.err != "" {
		t.Fatalf("quick spawn: %q", m.err)
	}
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d want 1", len(sessions))
	}
	if sessions[0].Tool != "claude-hooked" {
		t.Fatalf("spawned tool = %q want claude-hooked", sessions[0].Tool)
	}
}

func TestEditGroupRenamesAndSetsPath(t *testing.T) {
	m := buildModel(t)
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := m.store.CreateGroup("backend", oldDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}

	m.openRename()
	if m.mode != modeRename || !m.rename.isGroup {
		t.Fatalf("edit group should open, mode = %v", m.mode)
	}
	if m.rename.dir.Value() != oldDir {
		t.Fatalf("path prefill = %q want %q", m.rename.dir.Value(), oldDir)
	}
	m.rename.input.SetValue("platform")
	m.rename.dir.SetValue(newDir)
	if _, _ = m.applyRename(); m.err != "" {
		t.Fatalf("apply: %q", m.err)
	}
	m.applyCmd(t, m.refreshCmd())

	if m.groupPaths["platform"] != newDir {
		t.Fatalf("platform path = %q want %q", m.groupPaths["platform"], newDir)
	}
	if _, exists := m.groupPaths["backend"]; exists {
		t.Fatal("old group name should be gone")
	}
}

func TestEditGroupRejectsMissingPath(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("backend", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}
	m.openRename()
	m.rename.dir.SetValue("/nope/definitely/missing")
	if _, _ = m.applyRename(); m.err == "" {
		t.Fatal("missing path should be rejected")
	}
	if m.mode != modeRename {
		t.Fatal("modal should stay open on error")
	}
}

func TestGroupPathNeverEmpty(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	if m.groupForm.path.Value() == "" {
		t.Fatal("group form path should prefill with a resolved directory")
	}
	m.groupForm.name.SetValue("zone")
	m.groupForm.path.SetValue("")
	if _, _ = m.submitGroupForm(); m.err != "" {
		t.Fatalf("submit: %q", m.err)
	}
	m.applyCmd(t, m.refreshCmd())
	if m.groupPaths["zone"] == "" {
		t.Fatal("created group should get a resolved default path, not empty")
	}

	for i, row := range m.rows {
		if row.isGroup && row.group == "zone" {
			m.cursor = i
		}
	}
	m.openRename()
	if m.rename.dir.Value() == "" {
		t.Fatal("edit modal should prefill the path")
	}
	m.rename.dir.SetValue("")
	if _, _ = m.applyRename(); m.err != "" {
		t.Fatalf("apply: %q", m.err)
	}
	m.applyCmd(t, m.refreshCmd())
	if m.groupPaths["zone"] == "" {
		t.Fatal("edited group should keep a resolved path when cleared")
	}
}

func TestGroupRowRendersGroupPane(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "api-agent", dir, "backend")
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
		}
	}

	detail := ansi.Strip(m.viewDetail(112))
	if !strings.Contains(detail, dir) {
		t.Fatalf("group detail missing path %q:\n%s", dir, detail)
	}
	if !strings.Contains(detail, "1 agent") {
		t.Fatalf("group detail missing agent count:\n%s", detail)
	}

	agents := ansi.Strip(m.viewGroupAgents("backend", 112, 10))
	if !strings.Contains(agents, "api-agent") {
		t.Fatalf("agents list missing session:\n%s", agents)
	}

	inherited := ansi.Strip(m.viewGroupDetail("backend/sub", 112))
	if !strings.Contains(inherited, dir) || !strings.Contains(inherited, "inherited") {
		t.Fatalf("subgroup should inherit the ancestor path:\n%s", inherited)
	}
}

func TestCursorWrapsAroundTheList(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "first", dir, "")
	createSession(t, m, "second", dir, "")

	m.cursor = 0
	m.moveCursor(-1)
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("up from the top should wrap to the bottom, cursor = %d", m.cursor)
	}
	m.moveCursor(1)
	if m.cursor != 0 {
		t.Fatalf("down from the bottom should wrap to the top, cursor = %d", m.cursor)
	}

	m.rows = nil
	m.cursor = 0
	m.moveCursor(1)
	if m.cursor != 0 {
		t.Fatalf("empty list should leave the cursor alone, cursor = %d", m.cursor)
	}
}

func TestArchivedViewShowsOnlyArchivedSessions(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "live-one", dir, "")
	createSession(t, m, "old-one", dir, "")

	m.selectSessionRow(t, "old-one")
	_, cmd := m.archiveSelected()
	m.applyCmd(t, cmd)

	if names := sessionNames(m); len(names) != 1 || names[0] != "live-one" {
		t.Fatalf("active view = %v want [live-one]", names)
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if names := sessionNames(m); len(names) != 1 || names[0] != "old-one" {
		t.Fatalf("archived view = %v want [old-one]", names)
	}
}

func sessionNames(m *Model) []string {
	var names []string
	for _, sess := range m.sessionRows() {
		names = append(names, sess.Name)
	}
	return names
}
