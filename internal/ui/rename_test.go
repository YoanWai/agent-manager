package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/YoanWai/agent-manager/internal/store"
)

type refusingWorktreeBranchStore struct {
	*store.Store
	beforeRefusal func(string)
	err           error
}

func (s *refusingWorktreeBranchStore) RenameSessionWorktreeBranch(_ string, branch string) error {
	if s.beforeRefusal != nil {
		s.beforeRefusal(branch)
	}
	return s.err
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

func TestRenameSessionKeepsItsWorktreeDirectory(t *testing.T) {
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

	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != spawned.Cwd {
		t.Fatalf("stored cwd = %q, want spawn path %q", stored.Cwd, spawned.Cwd)
	}
	if stored.WorktreeBranch != "am/release-the-version" {
		t.Fatalf("stored branch = %q", stored.WorktreeBranch)
	}
	if stored.WorktreeRepo != spawned.WorktreeRepo {
		t.Fatalf("repo root moved: %q want %q", stored.WorktreeRepo, spawned.WorktreeRepo)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("spawn-time worktree directory moved: %v", err)
	}
	moved := filepath.Join(filepath.Dir(spawned.Cwd), "release-the-version")
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatalf("rename created a new worktree directory: %v", err)
	}
	if row := m.sessionRows()[0]; row.Cwd != spawned.Cwd || row.WorktreeBranch != "am/release-the-version" {
		t.Fatalf("row did not keep its path and follow the branch: %+v", row)
	}
	assertPaneStayedOnSpawnPath(t, m, spawned.ID, spawned.Cwd)
	head, err := exec.Command("git", "-C", spawned.Cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != "am/release-the-version" {
		t.Fatalf("spawn-time path HEAD = %q err=%v", strings.TrimSpace(string(head)), err)
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

func TestRenameSessionWorktreeBranchPutsItBackWhenTheStoreRefuses(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{Store: m.store, err: storeErr}
	sess := spawned
	err := renameSessionWorktreeBranch(m.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) {
		t.Fatalf("rename error = %v, want store refusal", err)
	}
	if sess.Cwd != spawned.Cwd || sess.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("session changed anyway: %+v", sess)
	}
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != spawned.Cwd || stored.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("stored session changed anyway: %+v", stored)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("worktree directory moved: %v", err)
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

func TestRenameSessionWorktreeBranchReportsRollbackNoOp(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{
		Store: m.store,
		err:   storeErr,
		beforeRefusal: func(branch string) {
			cmd := exec.Command("git", "-C", spawned.Cwd, "branch", "-m", branch, "feat/taken-over")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("take over branch: %v: %s", err, out)
			}
		},
	}
	sess := spawned
	err := renameSessionWorktreeBranch(m.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "rollback returned am/renamed instead of "+spawned.WorktreeBranch) {
		t.Fatalf("rename error = %v", err)
	}
	stored, getErr := m.store.Get(spawned.ID)
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	if stored.WorktreeBranch != spawned.WorktreeBranch || sess.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("store or session accepted the unrecorded branch: stored=%+v session=%+v", stored, sess)
	}
	head, err := exec.Command("git", "-C", spawned.Cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != "feat/taken-over" {
		t.Fatalf("taken-over worktree HEAD = %q err=%v", strings.TrimSpace(string(head)), err)
	}
}

func TestRenameSessionWorktreeBranchPreservesRollbackError(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{
		Store: m.store,
		err:   storeErr,
		beforeRefusal: func(string) {
			cmd := exec.Command("git", "-C", spawned.Cwd, "switch", "-c", "feat/taken-over")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("take over worktree: %v: %s", err, out)
			}
		},
	}
	sess := spawned
	err := renameSessionWorktreeBranch(m.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) {
		t.Fatalf("rename error = %v, want store refusal", err)
	}
	wrapped, ok := err.(interface{ Unwrap() []error })
	if !ok || len(wrapped.Unwrap()) != 2 {
		t.Fatalf("rename error does not preserve both causes: %v", err)
	}
	if rollbackErr := wrapped.Unwrap()[1]; !strings.Contains(rollbackErr.Error(), "not recorded branch am/renamed") {
		t.Fatalf("rollback error = %v", rollbackErr)
	}
}

func TestRenameDirtyWorktreeThenDeleteKeepsDirectory(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	m.rename.input.SetValue("dirty after")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("rename reported: %s", m.errBar.text)
	}
	if err := os.WriteFile(filepath.Join(spawned.Cwd, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleteSession(t, m, "dirty after")
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("dirty spawn-time worktree was removed: %v", err)
	}
	if !strings.Contains(m.errBar.text, spawned.Cwd) {
		t.Fatalf("error bar should name the kept path, got %q", m.errBar.text)
	}
}

func TestRenameThenSharedSessionKeepsSpawnPath(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	m.rename.input.SetValue("shared source")
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("rename reported: %s", m.errBar.text)
	}

	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != spawned.Cwd || stored.WorktreeBranch != "am/shared-source" {
		t.Fatalf("renamed session = %+v", stored)
	}

	forked := stored
	forked.ID = "shared-fork"
	forked.Name = "child fork"
	if err := m.tmux.Create(forked.ID, forked.Cwd, "cat", nil, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		t.Fatal(err)
	}
	if err := m.store.CreateSession(forked); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "shared source")
	m.openRename()
	m.rename.input.SetValue("should fail")
	m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.errBar.text, "shared with session \"child fork\"") {
		t.Fatalf("shared worktree error = %q", m.errBar.text)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("shared spawn-time directory moved: %v", err)
	}
	child, err := m.store.Get("shared-fork")
	if err != nil {
		t.Fatalf("get fork: %v", err)
	}
	if child.Cwd != spawned.Cwd || child.WorktreeBranch != "am/shared-source" {
		t.Fatalf("shared sibling lost the renamed worktree: %+v", child)
	}
}

func TestRenameThenDeleteRemovesTheSpawnWorktree(t *testing.T) {
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

	deleteSession(t, m, "release the version")
	if _, err := os.Stat(spawned.Cwd); !os.IsNotExist(err) {
		t.Fatal("clean spawn-time worktree should be removed after rename+delete")
	}
	out, err := exec.Command("git", "-C", repo, "branch", "--list", "am/release-the-version").Output()
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("renamed branch survived delete: %q", out)
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

func assertPaneStayedOnSpawnPath(t *testing.T, m *Model, id, want string) {
	t.Helper()
	got, err := m.tmux.PaneCurrentPath(id)
	if err != nil {
		t.Fatalf("pane path: %v", err)
	}
	wantRes, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve spawn path %q: %v", want, err)
	}
	gotRes, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("resolve pane cwd %q: %v", got, err)
	}
	if gotRes != wantRes {
		t.Fatalf("pane cwd = %q, want spawn path %q", got, want)
	}
}
