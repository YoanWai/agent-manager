package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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
				BusyLine:       `^[✻✳✶✽✢·✦✧+*] Waiting for \d+ background agents? to finish`,
				Rules: []config.Rule{
					{State: status.Waiting, Pattern: "Enter to confirm"},
					{State: status.Errored, Pattern: `(?im)^\s*error:`},
				},
			},
			"quietchat": {
				Command:        "cat",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^›",
			},
			"ready-tool": {
				Command:        `printf '❯ ' && cat`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			// Stands in for the agent CLIs, which turn on mouse tracking and
			// scroll themselves instead of leaving history for tmux.
			"mouse-tool": {
				Command:       `printf '\033[?1003h\033[?1006h' && cat`,
				DefaultStatus: status.Idle,
			},
			// Same claim on the mouse without asking for SGR, which is the
			// one case the reports have to fall back to the original
			// encoding.
			"x10-tool": {
				Command:       `printf '\033[?1003h' && cat`,
				DefaultStatus: status.Idle,
			},
		},
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	m := New(cfg, st, driver, engine, hooks.NewManager(t.TempDir()), "dev")
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

func (m *Model) selectGroupRow(t *testing.T, path string) {
	t.Helper()
	for i, r := range m.rows {
		if r.isGroup && r.group == path {
			m.cursor = i
			return
		}
	}
	t.Fatalf("no group row for %q", path)
}

// groupRowPaths lists the stored groups the tree paints, skipping root.
func (m *Model) groupRowPaths() []string {
	var paths []string
	for _, r := range m.rows {
		if r.isGroup && !r.isRoot() {
			paths = append(paths, r.group)
		}
	}
	return paths
}

func loadStoredRows(t *testing.T, m *Model) {
	t.Helper()
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	m.sessions = sessions
	m.groups = make([]string, len(groups))
	m.groupPaths = make(map[string]string, len(groups))
	m.archivedGroups = make(map[string]bool, len(groups))
	for i, group := range groups {
		m.groups[i] = group.Name
		m.groupPaths[group.Name] = group.Path
		if group.Archived {
			m.archivedGroups[group.Name] = true
		}
	}
	m.rebuildRows()
}

func listSessionIDs(t *testing.T, st *store.Store) []string {
	t.Helper()
	sessions, err := st.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	ids := make([]string, len(sessions))
	for i, sess := range sessions {
		ids[i] = sess.ID
	}
	return ids
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

func windowWidth(t *testing.T, id string) int {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{window_width}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v: %s", err, out)
	}
	w, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse width %q: %v", out, err)
	}
	return w
}

// A detached session must boot at the preview panel's width×height so its
// pane preview fills 1:1, and follow later terminal resizes, rather than
// staying at tmux's 80×24 default until attach.
func TestSessionSizesToPreviewPane(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	// Create sizes from pre-selection geometry; re-pin to the live preview box.
	m.resizeSessions()

	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("new session window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}

	m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	wantW, wantH = m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("after resize, window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
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

func TestArchivedViewIgnoresFold(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.collapsed["work"] = true

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("archived session inside a folded group should still show, got %d rows", len(m.sessionRows()))
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	active, err := m.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after restore, active sessions in store = %d want 1", len(active))
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

	groupPaths := m.groupRowPaths()
	want := []string{"backend", "backend/api", "backend/api/auth"}
	if len(groupPaths) != len(want) {
		t.Fatalf("group rows = %v want %v", groupPaths, want)
	}
	for i := range want {
		if groupPaths[i] != want[i] {
			t.Fatalf("group rows = %v want %v", groupPaths, want)
		}
	}

	if !m.rows[0].isRoot() {
		t.Fatalf("root row should lead the list, rows[0] = %+v", m.rows[0])
	}
	if m.rows[1].isGroup || m.rows[1].sess.Name != "top" {
		t.Fatalf("the top-level session should follow root, rows[1] = %+v", m.rows[1])
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

func TestPortableReorderKeysSwapVisibleSessions(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "keep-charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.search = "keep"
	loadStoredRows(t, m)
	m.selectSessionRow(t, "keep-charlie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	if got, want := []string{m.sessionRows()[0].ID, m.sessionRows()[1].ID}, []string{"c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("visible order after K = %v want %v", got, want)
	}
	if got, want := listSessionIDs(t, m.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after K = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.store), []string{"a", "hidden", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after J = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after shift+up = %v want %v", got, want)
	}
}

func TestReorderGroupSkipsFilteredSibling(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"alpha", "hidden", "gamma"} {
		if err := m.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Group: "alpha", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Group: "hidden", Status: "idle"},
		{ID: "g", Name: "keep-gamma", Tool: "claude", Cwd: "/tmp", Group: "gamma", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.search = "keep"
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	if got, want := m.groupRowPaths(), []string{"gamma", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("visible group order after K = %v want %v", got, want)
	}
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	got := make([]string, len(groups))
	for i, group := range groups {
		got[i] = group.Name
	}
	if want := []string{"gamma", "hidden", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("stored group order after K = %v want %v", got, want)
	}
}

func TestReorderSyntheticGroupUpdatesImmediately(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"alpha/deep", "beta/deep", "gamma/deep"} {
		if err := m.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	var roots []string
	for _, group := range m.groupRowPaths() {
		if !strings.Contains(group, "/") {
			roots = append(roots, group)
		}
	}
	if want := []string{"alpha", "gamma", "beta"}; !slices.Equal(roots, want) {
		t.Fatalf("root order after K = %v want %v", roots, want)
	}
}

func TestToggleEmptyGroupsFiltersTreeWithoutDeletingGroups(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"empty", "work", "work/leaf", "work/unused"} {
		if err := m.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "nested", t.TempDir(), "work/leaf")

	m.selectGroupRow(t, "empty")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd != nil {
		m.applyCmd(t, cmd)
	}

	wantVisible := []string{"work", "work/leaf"}
	if got := m.groupRowPaths(); !slices.Equal(got, wantVisible) {
		t.Fatalf("groups with empty subtrees should be hidden, got %v want %v", got, wantVisible)
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "show empty") {
		t.Fatalf("footer should offer the inverse action while filtered:\n%s", footer)
	}
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("list stored groups: %v", err)
	}
	if len(groups) != 4 {
		t.Fatalf("visual filter changed stored groups: got %d want 4", len(groups))
	}

	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	wantAll := []string{"empty", "work", "work/leaf", "work/unused"}
	if got := m.groupRowPaths(); !slices.Equal(got, wantAll) {
		t.Fatalf("second toggle should restore empty groups, got %v want %v", got, wantAll)
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "hide empty") {
		t.Fatalf("footer should offer hiding while empty groups are visible:\n%s", footer)
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

func TestPendingRenameForADeletedSessionDoesNotFailThePoll(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	sess := m.sessionRows()[0]

	// The manager deleted the row while this poll pass still held it.
	if err := m.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	nameFile := m.hooks.NameFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(nameFile), 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	if err := os.WriteFile(nameFile, []byte("renamed"), 0o644); err != nil {
		t.Fatalf("write name file: %v", err)
	}

	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename of a deleted session should not fail the pass: %v", err)
	}
	if _, found := m.hooks.ReadName(sess.ID); found {
		t.Fatal("the name file should be consumed instead of retried every poll")
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
	id := m.sessionRows()[0].ID
	if err := m.store.SetAgentSessionID(id, "conv-keep"); err != nil {
		t.Fatalf("set agent id: %v", err)
	}
	m.openRename()
	m.rename.input.SetValue("after")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got := m.sessionRows()[0]
	if got.Name != "after" {
		t.Fatalf("rename failed: %+v", got)
	}
	stored, err := m.store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.AgentSessionID != "conv-keep" {
		t.Fatalf("name-only rename wiped agent session id: %q", stored.AgentSessionID)
	}
	if stored.Tool != "claude" {
		t.Fatalf("name-only rename changed tool: %q", stored.Tool)
	}
}

func TestRenameSessionChangesTool(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "swapped", t.TempDir(), "")
	m.selectSessionRow(t, "swapped")
	start := m.sessionRows()[0]
	if start.Tool != "claude" {
		t.Fatalf("setup tool = %q want claude", start.Tool)
	}
	if err := m.store.SetAgentSessionID(start.ID, "conv-1"); err != nil {
		t.Fatalf("set agent id: %v", err)
	}
	m.openRename()
	if m.renameTool() != "claude" {
		t.Fatalf("rename tool start = %q want claude", m.renameTool())
	}
	if len(m.rename.toolNames) < 2 {
		t.Fatalf("need at least 2 tools to cycle, got %v", m.rename.toolNames)
	}
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyTab})
	wantTool := m.rename.toolNames[1]
	if m.renameTool() != wantTool {
		t.Fatalf("after tab tool = %q want %q", m.renameTool(), wantTool)
	}
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got := m.sessionRows()[0]
	if got.Tool != wantTool {
		t.Fatalf("tool after save = %q want %q", got.Tool, wantTool)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("agent session id should clear on tool change, got %q", got.AgentSessionID)
	}
	stored, err := m.store.Get(got.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Tool != wantTool || stored.AgentSessionID != "" {
		t.Fatalf("store after save: tool=%q agent=%q", stored.Tool, stored.AgentSessionID)
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

func TestHookWorkingReconcilesToFinishedOnEndedTurn(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked07", Tool: "claude-hooked", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "here is the result\n\n✻ Baked for 5s\n\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("stale working hook over an ended turn should reconcile to finished, got %q", got)
	}
}

// Claude fires Stop when the main agent stops responding, so a turn that
// leaves background agents running reports finished while they work. The
// pane still shows the wait, and that verdict has to win.
func TestHookFinishedUpgradesToWorkingWhileBackgroundAgentsRun(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked08", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "⏺ Security agent done. 2 left.\n✻ Waiting for 2 background agents to finish\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("background agents still running should upgrade hook finished to working, got %q", got)
	}

	sess.Acked = true
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("an acked session with background agents running should still read working, got %q", got)
	}
}

// The wait line disappears once the agents drain, and the completed turn
// below it must settle back to the hook's own verdict.
func TestHookFinishedStaysFinishedOnceBackgroundAgentsDrain(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked09", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "⏺ all agents reported\n✻ Worked for 5s\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("drained background agents should read finished, got %q", got)
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

func seedRegionHash(t *testing.T, m *Model, sess store.Session, pane string) {
	t.Helper()
	region, ok := m.poller.engine.ActivityRegion(sess.Tool, ansi.Strip(pane))
	if !ok {
		t.Fatal("pane should have an activity region")
	}
	m.poller.paneHashes = map[string]uint64{sess.ID: hashString(region)}
}

func disableQuietEndGrace(t *testing.T) {
	t.Helper()
	prev := quietEndGrace
	quietEndGrace = 0
	t.Cleanup(func() { quietEndGrace = prev })
}

func TestQuietPaneAfterWorkingDerivesFinished(t *testing.T) {
	disableQuietEndGrace(t)
	m := buildModel(t)
	sess := store.Session{ID: "quiet01", Tool: "claude-hooked", Status: status.Working}
	pane := "final answer with no turn marker\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("quiet pane after working should derive finished, got %q", got)
	}
}

func TestQuietPaneHoldsWorkingUntilGrace(t *testing.T) {
	prev := quietEndGrace
	quietEndGrace = time.Hour
	t.Cleanup(func() { quietEndGrace = prev })
	m := buildModel(t)
	sess := store.Session{ID: "quiet-hold", Tool: "claude-hooked", Status: status.Working}
	pane := "final answer with no turn marker\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("first quiet poll within grace should stay working, got %q", got)
	}
}

func TestQuietPaneEndingOnQuestionDerivesWaiting(t *testing.T) {
	disableQuietEndGrace(t)
	m := buildModel(t)
	sess := store.Session{ID: "quiet02", Tool: "claude-hooked", Status: status.Working}
	pane := "Which of the two options do you prefer?\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("quiet pane ending on a question should derive waiting, got %q", got)
	}
}

func TestQuietPaneAfterIdleStaysIdle(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "quiet03", Tool: "claude-hooked", Status: status.Idle}
	pane := "old transcript text\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("quiet pane after idle should stay idle, got %q", got)
	}
}

func TestQuietFinishedPersistsAndAckMapsToIdle(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "quiet04", Tool: "claude-hooked", Status: status.Finished}
	pane := "final answer with no turn marker\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("inferred finished should persist while the pane stays quiet, got %q", got)
	}
	sess.Acked = true
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("acked inferred finished should derive idle, got %q", got)
	}
}

func TestChangedRegionStillDerivesWorking(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "quiet05", Tool: "claude-hooked", Status: status.Working}
	seedRegionHash(t, m, sess, "earlier streaming text\n❯ \n")
	if got := deriveStatus(t, m, sess, "earlier streaming text plus more\n❯ \n", true); got != status.Working {
		t.Fatalf("changed region should derive working, got %q", got)
	}
}

// A post-resize rebaseline (no prior hash) must keep finished instead of
// inventing working from reflowed content or collapsing to idle.
func TestRebaselineKeepsFinishedWithoutFlashingWorking(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "reflow01", Tool: "claude-hooked", Status: status.Finished}
	before := "final answer line that wraps differently after resize\n❯ \n"
	after := "final answer line that wraps\ndifferently after resize\n❯ \n"
	seedRegionHash(t, m, sess, before)
	// Without clearing, a reflow looks like streaming work.
	if got := deriveStatus(t, m, sess, after, true); got != status.Working {
		t.Fatalf("reflow with a prior hash should look like working (precondition), got %q", got)
	}
	seedRegionHash(t, m, sess, before)
	m.poller.reflowSessions([]string{sess.ID}, func() {})
	if got := deriveStatus(t, m, sess, after, true); got != status.Finished {
		t.Fatalf("rebaseline after resize must keep finished, got %q", got)
	}
}

func TestRebaselineKeepsWaitingAndWorking(t *testing.T) {
	m := buildModel(t)
	pane := "Which option do you prefer?\n❯ \n"
	for _, st := range []string{status.Waiting, status.Working} {
		sess := store.Session{ID: "reflow-" + st, Tool: "claude-hooked", Status: st}
		if got := deriveStatus(t, m, sess, pane, true); got != st {
			t.Fatalf("unseen baseline with status %q: got %q", st, got)
		}
	}
}

func TestRebaselineIdleStaysIdle(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "reflow-idle", Tool: "claude-hooked", Status: status.Idle}
	pane := "old transcript text\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("unseen baseline idle should stay idle, got %q", got)
	}
}

func TestLiveQuietTurnResolvesFinished(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("quiet-live")
	m.form.dir.SetValue(t.TempDir())
	for i, name := range sortedToolNames(m.cfg) {
		if name == "quietchat" {
			m.form.toolIndex = i
		}
	}
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.err)
	}
	m.applyCmd(t, cmd)
	sess := m.sessionRows()[0]

	send := func(text string) {
		t.Helper()
		if err := m.tmux.SendText(sess.ID, text); err != nil {
			t.Fatalf("send %q: %v", text, err)
		}
	}
	waitStatus := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			m.applyCmd(t, m.refreshCmd())
			got, err := m.store.Get(sess.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Status == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("status = %q, want %q", got.Status, want)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	send("first answer chunk")
	send("› ask anything")
	m.applyCmd(t, m.refreshCmd())

	send("more streaming output")
	send("› ask anything")
	waitStatus(status.Working)
	waitStatus(status.Finished)
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
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.err)
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
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.err)
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
		t.Fatalf("attach did not start, err = %q", m.err)
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

func TestNewSessionShowsStartingImmediately(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("boot")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	pickGroup(t, m, "")
	// submitForm without the follow-up refresh: the row must already show the
	// launch state from the optimistic insert alone.
	if _, _ = m.submitForm(); m.err != "" {
		t.Fatalf("submit: %q", m.err)
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

	if _, _ = m.reviveAllDead(); m.err != "" {
		t.Fatalf("revive all: %q", m.err)
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

// pasteQuickImage runs a full ctrl+v paste against a fake clipboard that
// yields the given file, and returns the id of the chip it left behind.
func pasteQuickImage(t *testing.T, m *Model, path string) int {
	t.Helper()
	orig := captureClipboardImage
	defer func() { captureClipboardImage = orig }()
	captureClipboardImage = func() (string, error) { return path, nil }

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("ctrl+v should start an async clipboard read")
	}
	id := m.quick.lastImageID
	msg, ok := cmd().(quickImageMsg)
	if !ok {
		t.Fatalf("clipboard cmd returned %T", cmd())
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
	if m.err != "" {
		t.Fatalf("paste: %q", m.err)
	}
	return id
}

func tempImage(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func quickOffsetOf(t *testing.T, m *Model, needle string) int {
	t.Helper()
	idx := strings.Index(m.quick.input.Value(), needle)
	if idx < 0 {
		t.Fatalf("%q not in prompt %q", needle, m.quick.input.Value())
	}
	return utf8.RuneCountInString(m.quick.input.Value()[:idx])
}

func TestQuickPasteInsertsChipAtCursor(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	m.quick.input.SetValue("fix header and footer")
	m.quick.input.SetCursor(len("fix header"))
	path := tempImage(t, "paste-test.png")
	id := pasteQuickImage(t, m, path)

	want := "fix header " + imageToken(id) + " and footer"
	if got := m.quick.input.Value(); got != want {
		t.Fatalf("chip should land at the caret: got %q, want %q", got, want)
	}
	if len(m.quick.attachments) != 1 || m.quick.attachments[0].path != path {
		t.Fatalf("attachment = %+v", m.quick.attachments)
	}
	// Typing continues after the chip, leaving the text around it intact.
	for _, r := range " now" {
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.quick.input.Value(); got != "fix header "+imageToken(id)+" now and footer" {
		t.Fatalf("typing after the chip disturbed the text: %q", got)
	}
}

func TestQuickChipRendersInlineInTheInput(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := buildModel(t)
	m.openQuickMode()
	m.quick.attachments = []quickAttachment{{id: 1, path: "/tmp/agent-manager-pastes/paste-123.png"}}
	m.quick.input.SetValue("look at " + imageToken(1) + " please")
	m.quick.input.SetWidth(60)

	view := m.renderQuickChips(m.quick.input.View())
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "look at "+imageToken(1)+" please") {
		t.Fatalf("chip should read inline with the text, got %q", plain)
	}
	if strings.Contains(plain, "paste-123") {
		t.Fatalf("chip should not show the temp path, got %q", plain)
	}
	if plain == view {
		t.Fatal("chip should be styled in the rendered prompt")
	}
	// Styling must add color only: the token's width is what the textarea
	// already used to wrap the line.
	if lipgloss.Width(ansi.Strip(imageChip(imageToken(1)))) != lipgloss.Width(imageToken(1)) {
		t.Fatal("styled chip must keep the token's width")
	}

	m.quick.attachments = []quickAttachment{{id: 2}}
	m.quick.input.SetValue(imageToken(2))
	pending := m.renderQuickChips(m.quick.input.View())
	if !strings.Contains(pending, imageChipPasting(imageToken(2))) {
		t.Fatal("a chip waiting on the clipboard should wear the pasting style")
	}
}

func TestQuickMessageSubstitutesPathsInPlace(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	m.quick.attachments = []quickAttachment{{id: 1, path: "/tmp/a.png"}, {id: 2, path: "/tmp/b.png"}}
	m.quick.input.SetValue("compare " + imageToken(1) + " with " + imageToken(2))
	if got := m.quickMessage(); got != "compare /tmp/a.png with /tmp/b.png" {
		t.Fatalf("compose = %q", got)
	}

	m.quick.input.SetValue(imageToken(2) + " " + imageToken(1))
	if got := m.quickMessage(); got != "/tmp/b.png /tmp/a.png" {
		t.Fatalf("paths should follow the order they were pasted in: %q", got)
	}
}

func TestQuickBackspaceDeletesTheWholeChipAtTheCaret(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	first := tempImage(t, "first.png")
	second := tempImage(t, "second.png")
	m.quick.lastImageID = 2
	m.quick.attachments = []quickAttachment{{id: 1, path: first}, {id: 2, path: second}}
	m.quick.input.SetValue("this " + imageToken(1) + " and that " + imageToken(2))
	m.quick.input.SetCursor(quickOffsetOf(t, m, imageToken(1)) + utf8.RuneCountInString(imageToken(1)))

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.quick.input.Value(); got != "this  and that "+imageToken(2) {
		t.Fatalf("backspace beside a chip should remove it whole, got %q", got)
	}
	if len(m.quick.attachments) != 1 || m.quick.attachments[0].id != 2 {
		t.Fatalf("the other chip should stay: %+v", m.quick.attachments)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("deleted chip's temp file should be removed, err=%v", err)
	}
	if m.quickMessage() != "this  and that "+second {
		t.Fatalf("the surviving chip should still send its path: %q", m.quickMessage())
	}

	// Away from a chip, backspace is ordinary text editing.
	m.quick.input.CursorEnd()
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.quick.attachments) != 1 {
		t.Fatalf("plain backspace should leave chips alone: %+v", m.quick.attachments)
	}
}

func TestQuickDeleteRemovesTheChipInFront(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "forward.png")
	m.quick.lastImageID = 1
	m.quick.attachments = []quickAttachment{{id: 1, path: path}}
	m.quick.input.SetValue("head " + imageToken(1) + " tail")
	m.quick.input.SetCursor(quickOffsetOf(t, m, imageToken(1)))

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDelete})
	if got := m.quick.input.Value(); got != "head  tail" {
		t.Fatalf("delete in front of a chip should remove it whole, got %q", got)
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("attachment should be released: %+v", m.quick.attachments)
	}
}

func TestQuickArrowsStepOverAChip(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.lastImageID = 1
	m.quick.attachments = []quickAttachment{{id: 1, path: "/tmp/a.png"}}
	m.quick.input.SetValue("a " + imageToken(1) + " b")
	start := quickOffsetOf(t, m, imageToken(1))
	end := start + utf8.RuneCountInString(imageToken(1))
	m.quick.input.SetCursor(end)

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyLeft})
	if got := m.quickCursorOffset(); got != start {
		t.Fatalf("left should clear the chip in one step: offset %d, want %d", got, start)
	}
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRight})
	if got := m.quickCursorOffset(); got != end {
		t.Fatalf("right should clear the chip in one step: offset %d, want %d", got, end)
	}
}

func TestQuickChipsReflowAcrossWrappedRows(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.attachments = []quickAttachment{{id: 1, path: "/tmp/a.png"}}
	m.quick.input.SetWidth(24)
	m.quick.input.SetHeight(quickBarMaxRows)
	m.quick.input.SetValue(strings.Repeat("word ", 5) + imageToken(1) + " tail")

	rendered := ansi.Strip(m.renderQuickChips(m.quick.input.View()))
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) > 24 {
			t.Fatalf("wrapped row overflows the bar: %q", line)
		}
	}
	if !strings.Contains(rendered, imageToken(1)) {
		t.Fatalf("chip should survive wrapping, got %q", rendered)
	}
}

func TestQuickBulkDeleteReleasesTheImage(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "orphan.png")
	m.quick.lastImageID = 1
	m.quick.attachments = []quickAttachment{{id: 1, path: path}}
	m.quick.input.SetValue("note " + imageToken(1))
	m.quick.input.CursorEnd()

	// ctrl+u wipes the line without going through the chip-aware keys.
	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(m.quick.attachments) != 0 {
		t.Fatalf("a chip cut by a bulk delete should release its image: %+v", m.quick.attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("orphaned temp file should be removed, err=%v", err)
	}
}

func TestQuickPasteReservesTheChipUntilTheReadLands(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("ctrl+v should start an async clipboard read")
	}
	id := m.quick.lastImageID
	if !strings.Contains(m.quick.input.Value(), imageToken(id)) {
		t.Fatalf("the chip should hold its spot while the read runs, got %q", m.quick.input.Value())
	}
	if !m.quickPasting() {
		t.Fatal("the reserved chip should read as pasting")
	}
	// The caret sits after the chip, so typing keeps flowing.
	for _, r := range "later" {
		_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.quick.input.Value(); got != imageToken(id)+" later" {
		t.Fatalf("typing during a paste = %q", got)
	}
	_, _ = m.submitQuick()
	if m.err == "" {
		t.Fatal("submitting mid-paste should say the image is not ready")
	}

	path := tempImage(t, "landed.png")
	updated, _ := m.Update(quickImageMsg{id: id, path: path})
	m = updated.(*Model)
	if m.quickPasting() {
		t.Fatal("the chip should settle once the read lands")
	}
	if got := m.quick.input.Value(); got != imageToken(id)+" later" {
		t.Fatalf("the chip should resolve in place, got %q", got)
	}
	if got := m.quickMessage(); got != path+" later" {
		t.Fatalf("compose after the read = %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "png-bytes" {
		t.Fatalf("attachment file: %v %q", err, data)
	}
}

func TestQuickChipRemovalKeepsAMultiLinePrompt(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.lastImageID = 1
	m.quick.attachments = []quickAttachment{{id: 1, path: "/tmp/a.png"}}
	m.quick.input.SetValue("first line\nsecond " + imageToken(1) + " line\nthird line")
	m.quick.input.CursorUp()
	m.quick.input.SetCursor(len("second ") + utf8.RuneCountInString(imageToken(1)))
	if m.quick.input.Line() != 1 {
		t.Fatalf("test setup: caret on row %d", m.quick.input.Line())
	}

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.quick.input.Value(); got != "first line\nsecond  line\nthird line" {
		t.Fatalf("removing a chip should leave the other rows alone, got %q", got)
	}
	if m.quick.input.Line() != 1 || m.quickCursorOffset() != len("first line\nsecond ") {
		t.Fatalf("caret should stay where the chip was: row %d offset %d", m.quick.input.Line(), m.quickCursorOffset())
	}
}

func TestQuickPasteRefusedWhenThePromptIsFull(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetValue(strings.Repeat("x", m.quick.input.CharLimit))
	m.quick.input.CursorEnd()

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		t.Fatal("a full prompt should not start a clipboard read")
	}
	if m.err == "" {
		t.Fatal("a refused paste should say why")
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("no chip should be reserved: %+v", m.quick.attachments)
	}
	if strings.Contains(m.quick.input.Value(), "[Image") {
		t.Fatal("a truncated token must never reach the prompt")
	}
}

func TestQuickEscReleasesPastedImages(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	path := tempImage(t, "abandoned.png")
	m.quick.attachments = []quickAttachment{{id: 1, path: path}}
	m.quick.input.SetValue("never mind " + imageToken(1))

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.quick.attachments) != 0 {
		t.Fatalf("closing the bar should release its images: %+v", m.quick.attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("abandoned temp file should be removed, err=%v", err)
	}
}

func TestQuickImageMsgNoImageReachesTextPaste(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick.input.SetValue("keep this")
	m.quick.input.CursorEnd()

	_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	id := m.quick.lastImageID
	updated, cmd := m.Update(quickImageMsg{id: id, noImage: true})
	m = updated.(*Model)
	if m.quickPasting() || len(m.quick.attachments) != 0 {
		t.Fatalf("a clipboard with no image should take the chip back out: %+v", m.quick.attachments)
	}
	if got := m.quick.input.Value(); got != "keep this" {
		t.Fatalf("removing the chip should restore the typed text, got %q", got)
	}
	// Textarea paste returns a cmd for its own clipboard read, or nil if empty.
	_ = cmd
}

func TestQuickAttachImageRealErrorSurfaces(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick.input.SetValue("look")
	m.quick.input.CursorEnd()

	orig := captureClipboardImage
	defer func() { captureClipboardImage = orig }()
	captureClipboardImage = func() (string, error) {
		return "", errors.New("install wl-clipboard or xclip to paste images")
	}

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	msg := cmd().(quickImageMsg)
	updated, _ := m.Update(msg)
	m = updated.(*Model)
	if m.err == "" {
		t.Fatal("a real clipboard error should surface through m.err")
	}
	if m.quickPasting() || len(m.quick.attachments) != 0 {
		t.Fatalf("a failed read should take the chip back out: %+v", m.quick.attachments)
	}
	if got := m.quick.input.Value(); got != "look" {
		t.Fatalf("a failed read should leave the typed text, got %q", got)
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

func TestLaunchPromptInjectsDirectiveOnlyForAutoNamedWithPrompt(t *testing.T) {
	withDirective := launchPrompt("build the api", true)
	if !strings.HasPrefix(withDirective, renameDirective+"\n\n") || !strings.HasSuffix(withDirective, "build the api") {
		t.Fatalf("auto-named prompt should carry the directive, got %q", withDirective)
	}
	named := launchPrompt("build the api", false)
	if !strings.HasPrefix(named, renameAvailableNote+"\n\n") || !strings.HasSuffix(named, "build the api") {
		t.Fatalf("custom-named prompt should note rename is optional later, got %q", named)
	}
	if strings.Contains(named, "Run rename only this once") || strings.HasPrefix(named, renameDirective) {
		t.Fatalf("custom-named prompt must not force a rename, got %q", named)
	}
	if got := launchPrompt("", true); got != "" {
		t.Fatalf("promptless session should stay clean, got %q", got)
	}
	if got := launchPrompt("/compact keep the api notes", true); got != "/compact keep the api notes" {
		t.Fatalf("slash-command prompt should stay clean, got %q", got)
	}
	if got := launchPrompt("/compact keep the api notes", false); got != "/compact keep the api notes" {
		t.Fatalf("named slash-command prompt should stay clean, got %q", got)
	}
}

func TestSpawnMarksDeferredDirective(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.spawnSession("claude", "claude-aaaa", dir, "", "/compact", true); err != nil {
		t.Fatalf("slash spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	slashID := m.sessionRows()[0].ID
	if !m.poller.directivePending(slashID) {
		t.Fatal("slash-prompt spawn should defer the directive")
	}

	if err := m.spawnSession("claude", "claude-bbbb", dir, "", "do things", true); err != nil {
		t.Fatalf("plain spawn: %v", err)
	}
	if err := m.spawnSession("claude", "custom", dir, "", "/compact", false); err != nil {
		t.Fatalf("custom spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for _, sess := range m.sessionRows() {
		if sess.ID == slashID {
			continue
		}
		if m.poller.directivePending(sess.ID) {
			t.Fatalf("session %q should not defer a directive", sess.Name)
		}
	}
}

func TestDeferredDirectiveSentWhenPaneReady(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("ready-tool", "ready-tool-abcd", t.TempDir(), "", "", true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	sess := m.sessionRows()[0]
	// Launch scripts boot the tool immediately, so the first refresh may
	// already deliver the deferred directive. Either still-pending or
	// already present in the pane is success; a missing mark before any
	// send is not possible after spawnSession.
	deadline := time.Now().Add(5 * time.Second)
	for m.poller.directivePending(sess.ID) {
		if time.Now().After(deadline) {
			pane, _ := m.tmux.CapturePane(sess.ID)
			t.Fatalf("directive never sent; pane:\n%s", pane)
		}
		time.Sleep(100 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.Contains(pane, "agent-manager rename") {
		t.Fatalf("pane should hold the directive, got:\n%s", pane)
	}
}

func TestBuildLaunchCarriesSessionID(t *testing.T) {
	m := buildModel(t)
	plain := m.cfg.Tools["claude"]
	_, env, err := m.buildLaunch("plain", plain, plain.Command, "abcd1234")
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" {
		t.Fatalf("plain tool env = %v, want session id", env)
	}

	hooked := m.cfg.Tools["claude-hooked"]
	_, env, err = m.buildLaunch("hooked", hooked, hooked.Command, "abcd1234")
	if err != nil {
		t.Fatalf("buildLaunch hooked: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" || env[hooks.EnvStatusFile] == "" {
		t.Fatalf("hooked tool env = %v, want session id and status file", env)
	}
}

func writePendingName(t *testing.T, m *Model, id, name string) {
	t.Helper()
	path := m.hooks.NameFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatalf("write name: %v", err)
	}
}

func TestRefreshAppliesPendingRename(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "placeholder", t.TempDir(), "")
	sess := m.sessionRows()[0]

	writePendingName(t, m, sess.ID, "fix auth bug\n")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "fix auth bug" {
		t.Fatalf("name = %q, want the agent-chosen name", got.Name)
	}
	if _, err := os.Stat(m.hooks.NameFile(sess.ID)); !os.IsNotExist(err) {
		t.Fatal("applied name file should be consumed")
	}
}

func TestRefreshConsumesGarbageNameFile(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "keeper", t.TempDir(), "")
	sess := m.sessionRows()[0]

	writePendingName(t, m, sess.ID, "   \n")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "keeper" {
		t.Fatalf("whitespace name must not rename, got %q", got.Name)
	}
	if _, err := os.Stat(m.hooks.NameFile(sess.ID)); !os.IsNotExist(err) {
		t.Fatal("garbage name file should still be consumed")
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

	command, _, err := m.buildLaunch("claude", tool, withPrompt(tool, tool.Command, "fix the bug"), "prompt01")
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}
	if !strings.HasPrefix(command, "cat 'fix the bug' --mcp-config '") || !strings.Contains(command, "--settings '") {
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
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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

func TestArchiveRestoreClearStaleError(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")

	m.selectSessionRow(t, "alpha")
	m.err = "stale failure from an earlier action"
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.err != "" {
		t.Fatalf("archive should clear the stale error, err = %q", m.err)
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "alpha")
	m.err = "stale failure from an earlier action"
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.err != "" {
		t.Fatalf("restore should clear the stale error, err = %q", m.err)
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

	if m.err != "disk full" {
		t.Fatalf("snapshot failure should surface, err = %q", m.err)
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

func sessionNames(m *Model) []string {
	var names []string
	for _, sess := range m.sessionRows() {
		names = append(names, sess.Name)
	}
	return names
}

func gitTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiffReviewShowsWholeFile(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")

	m.drainCmds(t, m.openDiff())
	if !m.diff.active || m.mode != modeDiff || m.diff.loading {
		t.Fatalf("diff should be loaded fullscreen, active=%v mode=%v err=%q", m.diff.active, m.mode, m.diff.errText)
	}
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("files = %+v", m.diff.set.Files)
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "review · coder") || !strings.Contains(view, "files") {
		t.Fatalf("fullscreen review layout missing:\n%s", view)
	}
	if !strings.Contains(view, "package main") || !strings.Contains(view, "println(1)") {
		t.Fatalf("whole-file content missing:\n%s", view)
	}
	if !strings.Contains(view, "func main() {}") {
		t.Fatalf("deleted line should interleave:\n%s", view)
	}
}

func TestDiffScopeCycleAndLayout(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())

	m.applyCmd(t, m.cycleDiffScope())
	if m.diff.scope.String() != "vs target" {
		t.Fatalf("scope = %q", m.diff.scope)
	}

	m.diff.sideBySide = true
	if view := ansi.Strip(m.View()); !strings.Contains(view, "split") {
		t.Fatalf("split pill missing:\n%s", view)
	}
}

func TestDiffAnnotateAndSend(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	m.diff.sideBySide = false

	for i, fd := range m.diff.set.Files {
		if fd.File.Path == "main.go" {
			m.diff.fileIdx = i
		}
	}
	m.drainCmds(t, m.loadCurrentDiffFile())
	fd := m.currentFileDiff()
	target := -1
	for i, line := range fd.Lines {
		if line.NewNum > 0 && strings.Contains(line.Text, "println") {
			target = i
		}
	}
	if target < 0 {
		t.Fatalf("no add line found: %+v", fd.Lines)
	}
	m.diff.cursorLine = target
	m.openAnnotate()
	m.diff.annInput.SetValue("use fmt.Println here")
	m.saveAnnotation()
	if len(m.diff.annotations[m.reviewKey()]) != 1 {
		t.Fatalf("annotations = %+v", m.diff.annotations)
	}

	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	if len(m.diff.annotations[m.reviewKey()]) != 0 {
		t.Fatal("annotations should clear after send")
	}
	if !strings.Contains(m.diff.notice, "review comment") {
		t.Fatalf("feedback = %q", m.err)
	}
	sess := m.sessionRows()[0]
	// Join wrapped lines so the delivery check does not depend on where the
	// pane's width breaks the prompt; the session sizes to the model width.
	out, err := tmuxCmd("capture-pane", "-p", "-J", "-t", "am_"+sess.ID).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	pane := string(out)
	if !strings.Contains(pane, "use fmt.Println here") || !strings.Contains(pane, "main.go:3") {
		t.Fatalf("prompt not delivered:\n%s", pane)
	}
}

func TestCollapsedStatePersistsAcrossReload(t *testing.T) {
	m := buildModel(t)
	m.collapsed["backend"] = true
	m.collapsed["backend/api"] = true
	m.persistCollapsed()

	restored := loadCollapsed(m.store)
	if !restored["backend"] || !restored["backend/api"] {
		t.Fatalf("collapsed groups not restored: %v", restored)
	}

	m.collapsed["backend"] = false
	m.persistCollapsed()
	restored = loadCollapsed(m.store)
	if restored["backend"] {
		t.Fatalf("expanded group leaked back as collapsed: %v", restored)
	}
	if !restored["backend/api"] {
		t.Fatalf("still-folded group dropped: %v", restored)
	}
}

func TestToggleCollapseAllFlipsEveryGroup(t *testing.T) {
	m := buildModel(t)
	m.sessions = []store.Session{{ID: "a", Group: "backend/api"}, {ID: "b", Group: "frontend"}}
	want := []string{"backend", "backend/api", "frontend"}

	m.toggleCollapseAll()
	for _, group := range want {
		if !m.collapsed[group] {
			t.Fatalf("group %q not collapsed after fold-all", group)
		}
	}
	if restored := loadCollapsed(m.store); len(restored) != 3 {
		t.Fatalf("fold-all not persisted: %v", restored)
	}

	m.toggleCollapseAll()
	for _, group := range want {
		if m.collapsed[group] {
			t.Fatalf("group %q still collapsed after unfold-all", group)
		}
	}
	if restored := loadCollapsed(m.store); len(restored) != 0 {
		t.Fatalf("unfold-all not persisted: %v", restored)
	}
}

func TestDiffCommentVisibleInBothLayouts(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	m.diff.sideBySide = false

	for i, fd := range m.diff.set.Files {
		if fd.File.Path == "main.go" {
			m.diff.fileIdx = i
		}
	}
	m.drainCmds(t, m.loadCurrentDiffFile())
	fd := m.currentFileDiff()
	for i, line := range fd.Lines {
		if line.NewNum > 0 && strings.Contains(line.Text, "println") {
			m.diff.cursorLine = i
		}
	}
	m.openAnnotate()
	m.diff.annInput.SetValue("use fmt.Println here")
	m.saveAnnotation()

	m.diff.sideBySide = false
	if view := ansi.Strip(m.View()); !strings.Contains(view, "use fmt.Println here") {
		t.Fatalf("comment missing in unified layout:\n%s", view)
	}
	m.diff.sideBySide = true
	if view := ansi.Strip(m.View()); !strings.Contains(view, "use fmt.Println here") {
		t.Fatalf("comment missing in split layout:\n%s", view)
	}
}

func TestDefaultSplitLayout(t *testing.T) {
	m := buildModel(t)
	if !m.defaultSplitLayout() {
		t.Fatal("split should be the default layout")
	}
	if err := m.store.SetSetting(diffLayoutSetting, "unified"); err != nil {
		t.Fatal(err)
	}
	if m.defaultSplitLayout() {
		t.Fatal("stored unified choice should opt out of split")
	}
}

func TestQuickCloseAfterSendDefaultsToStayingOpen(t *testing.T) {
	m := buildModel(t)
	if m.quickCloseAfterSend() {
		t.Fatal("quick bar should stay open by default")
	}
	if err := m.store.SetSetting(quickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}
	if !m.quickCloseAfterSend() {
		t.Fatal("stored close choice should opt in")
	}
}

func TestQuickPromptClosesAfterSendWhenEnabled(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	if err := m.store.SetSetting(quickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}

	m.openQuickMode()
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.err != "" {
		t.Fatalf("send: %q", m.err)
	}
	if m.quick.active {
		t.Fatal("quick mode should close after a send when the setting is on")
	}
}

func TestSettingsTogglesQuickClose(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.quickCloseSend {
		t.Fatal("settings should open on stay-open by default")
	}
	for i := 0; i < settingsFieldQuickClose; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldQuickClose {
		t.Fatalf("stepping down should reach the quick send field, got %d", m.settings.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.quickCloseAfterSend() {
		t.Fatal("close choice should persist after toggle")
	}
}

func TestSettingsTogglesReviewLayout(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settings.layoutSplit {
		t.Fatal("settings should open on split by default")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.settings.field != settingsFieldTheme {
		t.Fatalf("first down should focus theme field, got %d", m.settings.field)
	}
	for i := settingsFieldTheme; i < settingsFieldLayout; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldLayout {
		t.Fatalf("stepping down should reach the layout field, got %d", m.settings.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.defaultSplitLayout(); got {
		t.Fatal("layout should persist as unified after toggle")
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
