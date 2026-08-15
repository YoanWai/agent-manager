package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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

func TestRenameSessionMovesItsWorktree(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	m.rename.input.SetValue("release the version")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("rename reported: %s", m.errBar.text)
	}

	// The spawn path is the one git resolved, which on macOS differs from
	// the temp path by the /private prefix.
	wantDir := filepath.Join(filepath.Dir(spawned.Cwd), "release-the-version")
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != wantDir {
		t.Fatalf("stored cwd = %q, want %q", stored.Cwd, wantDir)
	}
	if stored.WorktreeBranch != "am/release-the-version" {
		t.Fatalf("stored branch = %q", stored.WorktreeBranch)
	}
	if stored.WorktreeRepo != spawned.WorktreeRepo {
		t.Fatalf("repo root moved: %q want %q", stored.WorktreeRepo, spawned.WorktreeRepo)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("worktree directory did not follow the name: %v", err)
	}
	if _, err := os.Stat(spawned.Cwd); !os.IsNotExist(err) {
		t.Fatalf("old worktree directory survived: %v", err)
	}
	if row := m.sessionRows()[0]; row.Cwd != wantDir || row.WorktreeBranch != "am/release-the-version" {
		t.Fatalf("row still points at the old worktree: %+v", row)
	}
}

func TestRenameSessionRefusesSharedWorktree(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "owner", repo)
	forked := spawned
	forked.ID = "shared-fork"
	forked.Name = "forked"
	if err := m.store.CreateSession(forked); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "owner")
	m.openRename()
	m.rename.input.SetValue("renamed")
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})

	if !strings.Contains(m.errBar.text, "shared with session \"forked\"") {
		t.Fatalf("shared worktree error = %q", m.errBar.text)
	}
	if m.mode != modeRename {
		t.Fatalf("mode = %v, want rename card", m.mode)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("shared worktree moved: %v", err)
	}
}

func TestRenameSessionRefusesAWorktreeNameAlreadyTaken(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "mover", repo)
	createWorktreeSession(t, m, "taken", repo)

	m.selectSessionRow(t, "mover")
	m.openRename()
	m.rename.input.SetValue("taken")
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.errBar.text == "" {
		t.Fatal("a taken worktree name should report why")
	}
	if m.mode != modeRename {
		t.Fatalf("mode = %v, want the rename card still open to fix the name", m.mode)
	}
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "mover" || stored.Cwd != spawned.Cwd || stored.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("refused rename still moved something: %+v", stored)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("worktree should stay put: %v", err)
	}
}

func TestRenameSessionWorktreePutsItBackWhenTheStoreRefuses(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	// A store that cannot take the new location must not leave the session
	// recorded in a directory that has already moved away.
	m.store.Close()

	sess := spawned
	if err := renameSessionWorktree(m.gitDrv, m.store, &sess, "renamed"); err == nil {
		t.Fatal("a store that cannot record the move should report it")
	}
	if sess.Cwd != spawned.Cwd || sess.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("session moved anyway: %+v", sess)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("worktree was not put back: %v", err)
	}
	moved := filepath.Join(filepath.Dir(spawned.Cwd), "renamed")
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatalf("worktree left behind at the new path: %v", err)
	}
	out, err := exec.Command("git", "-C", repo, "branch", "--format=%(refname:short)").Output()
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	branches := strings.Fields(string(out))
	if !slices.Contains(branches, spawned.WorktreeBranch) {
		t.Fatalf("branch was not put back, have: %v", branches)
	}
	if slices.Contains(branches, "am/renamed") {
		t.Fatalf("rollback left the new branch behind, have: %v", branches)
	}
}

func TestRenameSessionWithoutAWorktreeIsUnchanged(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "plain", t.TempDir(), "")
	m.selectSessionRow(t, "plain")
	spawned := m.sessionRows()[0]

	m.openRename()
	m.rename.input.SetValue("still-plain")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "still-plain" || stored.Cwd != spawned.Cwd {
		t.Fatalf("shared-directory session should rename in place: %+v", stored)
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
	if _, _ = m.applyRename(); m.errBar.text != "" {
		t.Fatalf("apply: %q", m.errBar.text)
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
	if _, _ = m.applyRename(); m.errBar.text == "" {
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
	if _, _ = m.submitGroupForm(); m.errBar.text != "" {
		t.Fatalf("submit: %q", m.errBar.text)
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
	if _, _ = m.applyRename(); m.errBar.text != "" {
		t.Fatalf("apply: %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
	if m.groupPaths["zone"] == "" {
		t.Fatal("edited group should keep a resolved path when cleared")
	}
}

func TestRenameAgentToShellWithChildrenRefused(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.openRename()
	m.rename.input.SetValue("renamed")
	for i, name := range m.rename.toolNames {
		if name == "terminal" {
			m.rename.toolIndex = i
		}
	}
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("expected refuse")
	}
	got, _ := m.store.Get(m.sessionRows()[0].ID)
	if m.isShell(got.Tool) {
		t.Fatalf("tool became %q", got.Tool)
	}
	if got.Name != "coder" {
		t.Fatalf("name became %q", got.Name)
	}
}

func TestGroupEditPersistsWorktreeChoice(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("grp", t.TempDir()); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openRename()
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.rename.focus != 2 {
		t.Fatalf("focus should reach the worktree field, got %d", m.rename.focus)
	}
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Worktree != "on" {
		t.Fatalf("worktree choice should persist, got %+v", groups)
	}
	if !m.groupWorktree("grp") {
		t.Fatal("group worktree should resolve on")
	}
	if !m.groupWorktree("grp/child") {
		t.Fatal("child group should inherit the parent's worktree choice")
	}
}
