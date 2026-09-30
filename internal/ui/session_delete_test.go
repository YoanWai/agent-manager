package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteGroupSubtree(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("zone/inner", ""); err != nil {
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
	if err := m.services.store.SetArchived(archivedID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "zone" {
			setRailCursor(m, i)
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
		if m.services.tmux.Exists(id) {
			t.Fatalf("tmux session %s should be killed", id)
		}
	}
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Name != "outside" {
		t.Fatalf("only outside should remain, got %v", sessions)
	}
	all, _ := m.services.store.ListSessions(true)
	if len(all) != 1 {
		t.Fatalf("archived subtree session should be gone from db, got %d rows", len(all))
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "zone" || g.Name == "zone/inner" {
			t.Fatalf("group %s should be deleted", g.Name)
		}
	}
}

func TestDeleteGroupInArchivedViewSparesLiveSessions(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("bugs", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "old", dir, "bugs")
	createSession(t, m, "live", dir, "bugs")

	m.selectSessionRow(t, "old")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "bugs")
	m.prepareDelete()
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].Name != "old" {
		t.Fatalf("confirm should target only the archived session, got %+v", m.confirm.sessions)
	}
	archivedID := m.confirm.sessions[0].ID
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	if names := sessionNames(m); len(names) != 1 || names[0] != "live" {
		t.Fatalf("active view sessions = %v want [live]", names)
	}
	if paths := m.groupRowPaths(); len(paths) != 1 || paths[0] != "bugs" {
		t.Fatalf("group holding a live session should survive, got %v", paths)
	}
	for _, sess := range m.sessionRows() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("live session %s lost its tmux window", sess.Name)
		}
	}
	if m.services.tmux.Exists(archivedID) {
		t.Fatalf("archived session %s should be killed", archivedID)
	}
}

func TestDeleteArchivedGroupInArchivedViewRemovesIt(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.rail.SetArchived(true)
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
	sessions, _ := m.services.store.ListSessions(true)
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
	sessions, err := m.services.store.ListSessions(true)
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
	if err := m.services.tmux.Create(forked.ID, forked.Cwd, "cat", nil, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateSession(forked); err != nil {
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
	sessions, _ := m.services.store.ListSessions(true)
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
	if remaining, _ := m.services.store.ListSessions(true); len(remaining) != 0 {
		t.Fatal("session record should still be deleted")
	}
}

func TestDeleteAgentIncludesChildren(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.prepareDelete()
	ids := map[string]bool{}
	for _, sess := range m.confirm.sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("delete confirm omitted the child")
	}
	agent := m.sessionRows()[0]
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.applyCmd(t, cmd)
	for _, id := range []string{shell.ID, agent.ID} {
		if _, err := m.services.store.Get(id); err == nil {
			t.Fatalf("%s row survived the delete", id)
		}
		if m.services.tmux.Exists(id) {
			t.Fatalf("%s pane survived the delete", id)
		}
	}
}

func TestConfirmedDeleteDropsTheRowBeforeTheNextPoll(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	sess := m.sessionRows()[0]

	deleteSession(t, m, "doomed")

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("deleted session still on screen before the next poll: %+v", row)
		}
	}
}

func TestConfirmedGroupDeleteDropsTheGroupRowAtOnce(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")

	m.selectGroupRow(t, "zone")
	m.prepareDelete()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	for _, r := range railRows(m) {
		if r.isGroup && r.group == "zone" {
			t.Fatalf("deleted group still on screen before the next poll")
		}
	}
}
