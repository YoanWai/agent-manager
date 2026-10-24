package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// applyRename submits the open rename the way enter does.
func (m *Model) applyRename() (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

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
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.rail.SetCollapsed("old", true)
	m.rebuildRows()
	m.openRename()
	if !openRename(m).IsGroup() || openRename(m).Path() != "old" {
		t.Fatalf("rename target wrong: %+v", openRename(m))
	}
	openRename(m).SetName("fresh")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	kid := m.sessionRows()
	if len(kid) != 0 {
		t.Fatalf("fresh should stay collapsed after rename, got %d sessions", len(kid))
	}
	if !m.rail.IsCollapsed("fresh") || m.rail.IsCollapsed("old") {
		t.Fatalf("collapse state should follow rename: %v", m.rail.Collapsed())
	}
	m.rail.SetCollapsed("fresh", false)
	m.rebuildRows()
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "fresh/inner" {
		t.Fatalf("session group should cascade to fresh/inner, got %+v", sessions)
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if strings.HasPrefix(g.Name, "old") {
			t.Fatalf("old group path survived rename: %v", groups)
		}
	}
}

func TestRenameSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	id := m.sessionRows()[0].ID
	if err := m.services.store.SetAgentSessionID(id, "conv-keep"); err != nil {
		t.Fatalf("set agent id: %v", err)
	}
	m.openRename()
	openRename(m).SetName("after")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got := m.sessionRows()[0]
	if got.Name != "after" {
		t.Fatalf("rename failed: %+v", got)
	}
	stored, err := m.services.store.Get(id)
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
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	openRename(m).SetName("release the version")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("rename reported: %s", m.errBar.text)
	}

	stored, err := m.services.store.Get(spawned.ID)
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
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "owner", repo)
	forked := spawned
	forked.ID = "shared-fork"
	forked.Name = "forked"
	if err := m.services.store.CreateSession(forked); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "owner")
	m.openRename()
	openRename(m).SetName("renamed")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	if !strings.Contains(m.errBar.text, "shared with session \"forked\"") {
		t.Fatalf("shared worktree error = %q", m.errBar.text)
	}
	if !renameOpen(m) {
		t.Fatalf("screen = %v, want rename card", m.screenID())
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("shared worktree moved: %v", err)
	}
}

func TestRenameSessionRefusesAWorktreeNameAlreadyTaken(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "mover", repo)
	createWorktreeSession(t, m, "taken", repo)

	m.selectSessionRow(t, "mover")
	m.openRename()
	openRename(m).SetName("taken")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	if m.errBar.text == "" {
		t.Fatal("a taken worktree name should report why")
	}
	if !renameOpen(m) {
		t.Fatalf("screen = %v, want the rename card still open to fix the name", m.screenID())
	}
	stored, err := m.services.store.Get(spawned.ID)
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
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{Store: m.services.store, err: storeErr}
	sess := spawned
	err := sessioncmd.RenameWorktreeBranch(m.services.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) {
		t.Fatalf("rename error = %v, want store refusal", err)
	}
	if sess.Cwd != spawned.Cwd || sess.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("session changed anyway: %+v", sess)
	}
	stored, err := m.services.store.Get(spawned.ID)
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
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{
		Store: m.services.store,
		err:   storeErr,
		beforeRefusal: func(branch string) {
			cmd := exec.Command("git", "-C", spawned.Cwd, "branch", "-m", branch, "feat/taken-over")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("take over branch: %v: %s", err, out)
			}
		},
	}
	sess := spawned
	err := sessioncmd.RenameWorktreeBranch(m.services.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "rollback returned am/renamed instead of "+spawned.WorktreeBranch) {
		t.Fatalf("rename error = %v", err)
	}
	stored, getErr := m.services.store.Get(spawned.ID)
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
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{
		Store: m.services.store,
		err:   storeErr,
		beforeRefusal: func(string) {
			// The name the rollback wants is taken again in the meantime, so
			// git refuses to put the branch back.
			cmd := exec.Command("git", "-C", repo, "branch", spawned.WorktreeBranch)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("retake the old branch: %v: %s", err, out)
			}
		},
	}
	sess := spawned
	err := sessioncmd.RenameWorktreeBranch(m.services.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) {
		t.Fatalf("rename error = %v, want store refusal", err)
	}
	wrapped, ok := err.(interface{ Unwrap() []error })
	if !ok || len(wrapped.Unwrap()) != 2 {
		t.Fatalf("rename error does not preserve both causes: %v", err)
	}
	if rollbackErr := wrapped.Unwrap()[1]; !strings.Contains(rollbackErr.Error(), "branch already exists: "+spawned.WorktreeBranch) {
		t.Fatalf("rollback error = %v", rollbackErr)
	}
}

// A worktree switched off the branch mid-rename is left where it is, so
// the rollback reports what it found rather than moving someone else's
// checkout back.
func TestRenameSessionWorktreeBranchReportsASwitchedRollback(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	storeErr := errors.New("store refused branch")
	refusingStore := &refusingWorktreeBranchStore{
		Store: m.services.store,
		err:   storeErr,
		beforeRefusal: func(string) {
			cmd := exec.Command("git", "-C", spawned.Cwd, "switch", "-c", "feat/taken-over")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("take over worktree: %v: %s", err, out)
			}
		},
	}
	sess := spawned
	err := sessioncmd.RenameWorktreeBranch(m.services.gitDrv, refusingStore, &sess, "renamed")
	if !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "rollback returned am/renamed instead of "+spawned.WorktreeBranch) {
		t.Fatalf("rename error = %v", err)
	}
	if sess.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("session took a branch the store never recorded: %+v", sess)
	}
	head, err := exec.Command("git", "-C", spawned.Cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != "feat/taken-over" {
		t.Fatalf("taken-over worktree HEAD = %q err=%v", strings.TrimSpace(string(head)), err)
	}
}

func TestRenameDirtyWorktreeThenDeleteKeepsDirectory(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	openRename(m).SetName("dirty after")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	openRename(m).SetName("shared source")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("rename reported: %s", m.errBar.text)
	}

	stored, err := m.services.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != spawned.Cwd || stored.WorktreeBranch != "am/shared-source" {
		t.Fatalf("renamed session = %+v", stored)
	}

	forked := stored
	forked.ID = "shared-fork"
	forked.Name = "child fork"
	if err := m.services.tmux.Create(forked.ID, forked.Cwd, "cat", nil, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateSession(forked); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "shared source")
	m.openRename()
	openRename(m).SetName("should fail")
	_, retryCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, retryCmd)
	if !strings.Contains(m.errBar.text, "shared with session \"child fork\"") {
		t.Fatalf("shared worktree error = %q", m.errBar.text)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("shared spawn-time directory moved: %v", err)
	}
	child, err := m.services.store.Get("shared-fork")
	if err != nil {
		t.Fatalf("get fork: %v", err)
	}
	if child.Cwd != spawned.Cwd || child.WorktreeBranch != "am/shared-source" {
		t.Fatalf("shared sibling lost the renamed worktree: %+v", child)
	}
}

func TestRenameThenDeleteRemovesTheSpawnWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	m.selectSessionRow(t, "claude-7a72")
	m.openRename()
	openRename(m).SetName("release the version")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "plain", t.TempDir(), "")
	m.selectSessionRow(t, "plain")
	spawned := m.sessionRows()[0]

	m.openRename()
	openRename(m).SetName("still-plain")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	stored, err := m.services.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "still-plain" || stored.Cwd != spawned.Cwd {
		t.Fatalf("shared-directory session should rename in place: %+v", stored)
	}
}

func TestRenameSessionChangesTool(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "swapped", t.TempDir(), "")
	m.selectSessionRow(t, "swapped")
	start := m.sessionRows()[0]
	if start.Tool != "claude" {
		t.Fatalf("setup tool = %q want claude", start.Tool)
	}
	if err := m.services.store.SetAgentSessionID(start.ID, "conv-1"); err != nil {
		t.Fatalf("set agent id: %v", err)
	}
	m.openRename()
	if openRename(m).Tool() != "claude" {
		t.Fatalf("rename tool start = %q want claude", openRename(m).Tool())
	}
	if len(openRename(m).Tools()) < 2 {
		t.Fatalf("need at least 2 tools to cycle, got %v", openRename(m).Tools())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	wantTool := openRename(m).Tools()[1]
	if openRename(m).Tool() != wantTool {
		t.Fatalf("after tab tool = %q want %q", openRename(m).Tool(), wantTool)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got := m.sessionRows()[0]
	if got.Tool != wantTool {
		t.Fatalf("tool after save = %q want %q", got.Tool, wantTool)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("agent session id should clear on tool change, got %q", got.AgentSessionID)
	}
	stored, err := m.services.store.Get(got.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Tool != wantTool || stored.AgentSessionID != "" {
		t.Fatalf("store after save: tool=%q agent=%q", stored.Tool, stored.AgentSessionID)
	}
}

func TestEditGroupRenamesAndSetsPath(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", oldDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}

	m.openRename()
	if !renameOpen(m) || !openRename(m).IsGroup() {
		t.Fatalf("edit group should open, screen = %v", m.screenID())
	}
	if openRename(m).Dir() != oldDir {
		t.Fatalf("path prefill = %q want %q", openRename(m).Dir(), oldDir)
	}
	openRename(m).SetName("platform")
	openRename(m).SetDir(newDir)
	if _, _ = m.applyRename(); m.errBar.text != "" {
		t.Fatalf("apply: %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())

	if m.workspace.groupPaths["platform"] != newDir {
		t.Fatalf("platform path = %q want %q", m.workspace.groupPaths["platform"], newDir)
	}
	if _, exists := m.workspace.groupPaths["backend"]; exists {
		t.Fatal("old group name should be gone")
	}
}

func TestEditGroupRejectsMissingPath(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("backend", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	openRename(m).SetDir("/nope/definitely/missing")
	_, cmd := m.applyRename()
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("missing path should be rejected")
	}
	if !renameOpen(m) {
		t.Fatal("modal should stay open on error")
	}
}

func TestGroupPathNeverEmpty(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openGroupForm()
	if m.groupSurface().Dir() == "" {
		t.Fatal("group form path should prefill with a resolved directory")
	}
	m.groupSurface().SetName("zone")
	m.groupSurface().SetDir("")
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("submit: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.workspace.groupPaths["zone"] == "" {
		t.Fatal("created group should get a resolved default path, not empty")
	}

	for i, row := range railRows(m) {
		if row.isGroup && row.group == "zone" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	if openRename(m).Dir() == "" {
		t.Fatal("edit modal should prefill the path")
	}
	openRename(m).SetDir("")
	if _, _ = m.applyRename(); m.errBar.text != "" {
		t.Fatalf("apply: %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
	if m.workspace.groupPaths["zone"] == "" {
		t.Fatal("edited group should keep a resolved path when cleared")
	}
}

func TestRenameAgentToShellWithChildrenRefused(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.openRename()
	openRename(m).SetName("renamed")
	for range openRename(m).Tools() {
		if openRename(m).Tool() == "terminal" {
			break
		}
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("expected refuse")
	}
	got, _ := m.services.store.Get(m.sessionRows()[0].ID)
	if m.isShell(got.Tool) {
		t.Fatalf("tool became %q", got.Tool)
	}
	if got.Name != "coder" {
		t.Fatalf("name became %q", got.Name)
	}
}

func TestGroupEditPersistsWorktreeChoice(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("grp", t.TempDir()); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openRename()
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if openRename(m).Focus() != 2 {
		t.Fatalf("focus should reach the worktree field, got %d", openRename(m).Focus())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Worktree != "on" {
		t.Fatalf("worktree choice should persist, got %+v", groups)
	}
	if !m.cachedSpawnWorktreeDefault("grp") {
		t.Fatal("group worktree should resolve on")
	}
	if !m.cachedSpawnWorktreeDefault("grp/child") {
		t.Fatal("child group should inherit the parent's worktree choice")
	}
}

func TestGroupEditPersistsBase(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("grp", repoWithDevelop(t)); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openRename()
	for range 3 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if openRename(m).Focus() != 3 {
		t.Fatalf("focus should reach the base field, got %d", openRename(m).Focus())
	}
	_, step := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.applyTestMsg(t, step())
	if view := m.viewGroupDetail("grp", 80); !strings.Contains(view, "◂ develop ▸") {
		t.Fatalf("base row should show the pick, got %q", view)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Base != "develop" {
		t.Fatalf("base should persist, got %+v", groups)
	}
	if m.groupBase("grp/child") != "develop" {
		t.Fatal("child group should inherit the parent's base")
	}
}

func assertPaneStayedOnSpawnPath(t *testing.T, m *Model, id, want string) {
	t.Helper()
	got, err := m.services.tmux.PaneCurrentPath(id)
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

func TestGroupedSessionRenameEditorIsVisible(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 100, 30
	if err := m.services.store.CreateGroup("work/inner", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "before", t.TempDir(), "work/inner")
	m.selectSessionRow(t, "before")
	m.openRename()
	openRename(m).SetName("edited-name")
	if frame := preparedView(m); !strings.Contains(frame, "edited-name") {
		t.Fatalf("grouped session editor is absent from frame:\n%s", frame)
	}
}

func renameOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.RenameSurface](m)
	return ok
}

// openRename is the rename on the stack; a test that edits one without
// opening it fails here, not on a nil.
func openRename(m *Model) *uidialogs.RenameSurface {
	s, ok := surfaceOf[*uidialogs.RenameSurface](m)
	if !ok {
		panic("no rename is open")
	}
	return s
}

func openRenameFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.openRename()
	if !renameOpen(m) {
		t.Fatalf("test setup did not open the rename, err %q", m.errBar.text)
	}
	return m
}

// The name is typed into its row, so the surface paints no card, keeps its
// draft through a stray click and leaves the list where it is.
func TestRenameIsARowNotACard(t *testing.T) {
	m := openRenameFor(t)
	if mustTop(t, m).Dismissible() {
		t.Fatal("a rename holds a typed draft and must not close on a stray click")
	}
	m.prepareFrame()
	if m.layout.card != (cardRect{}) {
		t.Fatalf("rename painted a card: %+v", m.layout.card)
	}
	if m.cardOpen() != true {
		// cardOpen follows the stack; routeMouse still holds the list still.
		t.Fatal("the stack's top is not a card")
	}
	if m = leftClick(m, 0, 0); !renameOpen(m) {
		t.Fatal("a click closed the rename draft")
	}
}

func TestRenameEscClosesAndCtrlCQuits(t *testing.T) {
	m := openRenameFor(t)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if renameOpen(m) || m.effects.Quitting || !m.onList() {
		t.Fatalf("esc: open=%t quitting=%t screen = %v", renameOpen(m), m.effects.Quitting, m.screenID())
	}
	m = openRenameFor(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.effects.Quitting {
		t.Fatal("ctrl+c over the list did not quit")
	}
}

// A completion for a rename that was popped, or for one reopened since, is
// dropped.
func TestStaleRenameCompletionIsDropped(t *testing.T) {
	m := openRenameFor(t)
	gen := m.gens.dialog
	name := openRename(m).Name()
	m.Pop()
	m.openRename()
	m.gens.dialog++
	job := &uieffects.Job{Request: uiapply.RenameEffect{Request: uiapply.Request{Kind: uiapply.Session, SessID: openRename(m).SessionID(), Name: name, Gen: gen}}}
	m.applyRenameOutcome(job, uiapply.RenameEffectResult{}, nil)
	if !renameOpen(m) {
		t.Fatal("a completion from an older rename closed the newer one")
	}
}

var errRenameStageTwo = errors.New("second stage failed")

func TestRenameDefersGroupPersistence(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	if !openRename(m).IsGroup() || openRename(m).Path() != "old" {
		t.Fatalf("rename target wrong: %+v", openRename(m))
	}
	openRename(m).SetName("fresh")
	openRename(m).SetDir(dir)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("group rename dispatch returned no command")
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "fresh" || g.Name == "fresh/inner" {
			t.Fatalf("group rename persisted on the update path: %v", groups)
		}
	}
	if !renameOpen(m) {
		t.Fatalf("dialog should stay open until completion, screen = %v", m.screenID())
	}
	m.applyCmd(t, cmd)
	kid := m.sessionRows()
	if len(kid) != 1 || kid[0].Group != "fresh/inner" {
		t.Fatalf("cascade failed: %+v", kid)
	}
	if !m.onList() {
		t.Fatalf("dialog should close on completion, screen = %v", m.screenID())
	}
	if m.workspace.groupPaths["fresh"] != dir {
		t.Fatalf("default dir not mirrored: %v", m.workspace.groupPaths)
	}
}

func TestRenameDefersSessionPersistence(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	m.openRename()
	openRename(m).SetName("after")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("session rename dispatch returned no command")
	}
	if names := sessionNames(m); len(names) != 1 || names[0] != "before" {
		t.Fatalf("row renamed on the update path: %v", names)
	}
	m.applyCmd(t, cmd)
	if names := sessionNames(m); len(names) != 1 || names[0] != "after" {
		t.Fatalf("row not renamed on completion: %v", names)
	}
	if !m.onList() {
		t.Fatalf("dialog should close on completion, screen = %v", m.screenID())
	}
}

func TestRenameShellChildCheckRunsInWorker(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.openRename()
	openRename(m).SetName("renamed")
	for range openRename(m).Tools() {
		if openRename(m).Tool() == "terminal" {
			break
		}
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, cmd := m.applyRename()
	if cmd == nil {
		t.Fatal("rename did not return the queued worker command")
	}
	if m.errBar.text != "" {
		t.Fatalf("child store read ran on the update path: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.errBar.text != "move its terminals first" {
		t.Fatalf("worker child precondition = %q", m.errBar.text)
	}
	stored, err := m.services.store.Get(openRename(m).SessionID())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "coder" || m.isShell(stored.Tool) {
		t.Fatalf("failed precondition mutated the session: %+v", stored)
	}
}

func TestRenameGroupDirectoryCheckRunsBeforeMutation(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("old", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "old")
	m.openRename()
	openRename(m).SetName("new")
	openRename(m).SetDir("/definitely/missing/agent-manager")
	_, cmd := m.applyRename()
	groups, _ := m.services.store.Groups()
	if len(groups) != 1 || groups[0].Name != "old" {
		t.Fatalf("rename mutated before directory preflight: %+v", groups)
	}
	m.applyCmd(t, cmd)
	groups, _ = m.services.store.Groups()
	if len(groups) != 1 || groups[0].Name != "old" {
		t.Fatalf("failed directory preflight mutated group: %+v", groups)
	}
	if !strings.Contains(m.errBar.text, "default path does not exist") {
		t.Fatalf("directory preflight error = %q", m.errBar.text)
	}
}

func TestRenameKeepsCapturedRowIdentity(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "captured", t.TempDir(), "")
	m.selectSessionRow(t, "captured")
	m.openRename()
	openRename(m).SetName("saved")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for i := range m.workspace.sessions {
		m.workspace.sessions[i].Name = "mutated"
	}
	m.applyCmd(t, cmd)
	if names := sessionNames(m); len(names) != 1 || names[0] != "saved" {
		t.Fatalf("completion used the live row, want the captured one: %v", names)
	}
}

func TestRenameChainedWhileQueuedActsOnThePreviousResult(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "alpha", repo)

	m.selectSessionRow(t, "alpha")
	m.openRename()
	openRename(m).SetName("beta")
	_, first := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	openRename(m).SetName("gamma")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, first)
	if m.errBar.text != "" {
		t.Fatalf("rename reported: %s", m.errBar.text)
	}

	stored, err := m.services.store.Get(spawned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "gamma" || stored.WorktreeBranch != "am/gamma" {
		t.Fatalf("stored name/branch = %q/%q, want gamma/am/gamma", stored.Name, stored.WorktreeBranch)
	}
	if row := m.sessionRows()[0]; row.Name != "gamma" || row.WorktreeBranch != "am/gamma" {
		t.Fatalf("row name/branch = %q/%q, want gamma/am/gamma", row.Name, row.WorktreeBranch)
	}
	head, err := exec.Command("git", "-C", spawned.Cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != "am/gamma" {
		t.Fatalf("worktree HEAD = %q err=%v", strings.TrimSpace(string(head)), err)
	}
}

func TestRenameFollowsARelaunchButRefusesAReplacedRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	m.openRename()
	openRename(m).SetName("after")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	id := openRename(m).SessionID()
	if err := m.services.store.SetAgentLaunchedAt(id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	if stored, err := m.services.store.Get(id); err != nil || stored.Name != "after" {
		t.Fatalf("relaunched session not renamed: %q, %v", stored.Name, err)
	}
	m.selectSessionRow(t, "after")
	m.openRename()
	openRename(m).SetName("replaced")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if err := m.services.store.SetTmuxSocket(id, "elsewhere"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "was replaced") {
		t.Fatalf("replaced target error = %q", m.errBar.text)
	}
	if stored, err := m.services.store.Get(id); err != nil || stored.Name != "after" {
		t.Fatalf("replaced target renamed to %q, %v", stored.Name, err)
	}
}

func TestRenameGroupPartialFailureReconcilesCommittedStages(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	request := uiapply.RenameEffect{Request: uiapply.Request{
		Kind:     uiapply.Group,
		OldGroup: "old",
		NewGroup: "fresh",
		Dir:      dir,
		Name:     "fresh",
		Gen:      7,
	}}
	job := &uieffects.Job{Request: request}
	m.push(uidialogs.NewRenameSurface(renameRoot{m}, uidialogs.RenameGroup("old", "", 0), ""))
	m.gens.dialog = 7
	m.applyRenameOutcome(job, uiapply.RenameEffectResult{Outcome: uiapply.Outcome{GroupRenamed: true}}, errRenameStageTwo)

	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "old" {
			t.Fatalf("committed rename lost: %v", groups)
		}
	}
	kid := m.sessionRows()
	if len(kid) != 1 || kid[0].Group != "fresh/inner" {
		t.Fatalf("committed rename not mirrored locally: %+v", kid)
	}
	if m.workspace.groupPaths["fresh"] != "" {
		t.Fatalf("uncommitted default dir mirrored: %v", m.workspace.groupPaths)
	}
	if !renameOpen(m) {
		t.Fatalf("dialog should stay open on failure, screen = %v", m.screenID())
	}
	if m.errBar.text != "second stage failed" {
		t.Fatalf("errBar=%q", m.errBar.text)
	}
	var renderedFresh bool
	for _, r := range railRows(m) {
		if r.isGroup && r.group == "fresh" {
			renderedFresh = true
		}
	}
	if !renderedFresh {
		t.Fatal("committed rename not rendered before the error")
	}
}

func TestRenameCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"one", "two"} {
		if err := m.services.store.CreateGroup(name, ""); err != nil {
			t.Fatalf("create group: %v", err)
		}
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "one")
	m.openRename()
	openRename(m).SetName("one-renamed")
	_, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first rename dispatch returned no command")
	}
	m.selectGroupRow(t, "two")
	m.openRename()
	openRename(m).SetName("two-renamed")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if !renameOpen(m) || openRename(m).Path() != "two" {
		t.Fatalf("older completion replaced the newer dialog: screen = %v path=%q", m.screenID(), openRename(m).Path())
	}
	m.drainEffects(t)
	groups, _ := m.services.store.Groups()
	renamed := map[string]bool{}
	for _, g := range groups {
		renamed[g.Name] = true
	}
	if !renamed["one-renamed"] || !renamed["two-renamed"] {
		t.Fatalf("expected both renames committed: %v", groups)
	}
	if !m.onList() {
		t.Fatalf("dialog should close on the matching completion, screen = %v", m.screenID())
	}
}

func TestRenameCommandExecutesOnceAndCompletionIsIdempotent(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "once", t.TempDir(), "")
	m.selectSessionRow(t, "once")
	m.openRename()
	openRename(m).SetName("twice")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	first := cmd()
	second := cmd()
	firstMsg, ok := first.(uieffects.Completed)
	if !ok {
		t.Fatalf("first run returned %+v", first)
	}
	secondMsg, ok := second.(uieffects.Completed)
	if !ok {
		t.Fatalf("second run returned %+v", second)
	}
	if firstMsg.ID != secondMsg.ID {
		t.Fatal("second run produced a different completion")
	}
	updated, _ := m.Update(first)
	*m = *updated.(*Model)
	updated, _ = m.Update(second)
	*m = *updated.(*Model)
	if names := sessionNames(m); len(names) != 1 || names[0] != "twice" {
		t.Fatalf("completion applied twice: %v", names)
	}
	stored, _ := m.services.store.ListSessions(false)
	if len(stored) != 1 || stored[0].Name != "twice" {
		t.Fatalf("store renamed twice: %+v", stored)
	}
}

func TestQuitDrainsRenameEffects(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")
	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	openRename(m).SetName("fresh")
	openRename(m).SetDir(dir)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("group rename dispatch returned no command")
	}
	_, _ = m.requestQuit()
	if !m.effects.Quitting {
		t.Fatal("quit not queued")
	}
	m.applyCmd(t, cmd)
	m.drainEffects(t)
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "old" {
			t.Fatalf("quit drained the queue without committing: %v", groups)
		}
	}
}

func TestMoveSessionDefersPlacement(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	moved := movingSession(m)
	pickGroup(t, m, "target")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move dispatch returned no command")
	}
	got, err := m.services.store.Get(moved)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != "" {
		t.Fatalf("session placed on the update path: %+v", got)
	}
	if !moveOpen(m) {
		t.Fatalf("dialog should stay open until completion, screen = %v", m.screenID())
	}
	m.applyCmd(t, cmd)
	got, _ = m.services.store.Get(moved)
	if got.Group != "target" {
		t.Fatalf("move not committed on completion: %+v", got)
	}
	if !m.onList() {
		t.Fatalf("dialog should close on completion, screen = %v", m.screenID())
	}
}

func TestMoveWorkerRejectsChangedSourcePrecondition(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"target", "concurrent"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatal(err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	moved := movingSession(m)
	pickGroup(t, m, "target")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if err := m.services.store.PlaceSession(moved, "concurrent", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	stored, err := m.services.store.Get(moved)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Group != "concurrent" {
		t.Fatalf("stale move overwrote concurrent placement: %+v", stored)
	}
	if m.errBar.text != "move source changed before placement" {
		t.Fatalf("precondition error = %q", m.errBar.text)
	}
}

func TestMoveGroupDefersPersistence(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.services.store.CreateGroup("beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "alpha/inner")

	m.selectGroupRow(t, "alpha/inner")
	m.openMove()
	pickGroup(t, m, "beta")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move dispatch returned no command")
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "beta/inner" {
			t.Fatalf("group moved on the update path: %v", groups)
		}
	}
	m.applyCmd(t, cmd)
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "beta/inner" {
		t.Fatalf("group move failed: %+v", sessions)
	}
	if !m.onList() {
		t.Fatalf("dialog should close on completion, screen = %v", m.screenID())
	}
}

func TestMoveCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	for _, name := range []string{"target", "other"} {
		if err := m.services.store.CreateGroup(name, ""); err != nil {
			t.Fatalf("create group: %v", err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	moved := movingSession(m)
	pickGroup(t, m, "target")
	_, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first move dispatch returned no command")
	}
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	pickGroup(t, m, "other")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The lane runs four jobs: place target, close target dialog (must not
	// match), place other, close other dialog.
	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if !moveOpen(m) {
		t.Fatalf("older completion closed the newer dialog, screen = %v", m.screenID())
	}
	if picker(m).Groups[picker(m).GroupIndex].Path != "other" {
		t.Fatalf("newer dialog selection changed: %+v", picker(m).Groups)
	}
	m.drainEffects(t)
	got, err := m.services.store.Get(moved)
	if err != nil {
		t.Fatal(err)
	}
	if got.Group != "other" {
		t.Fatalf("second move not committed: %+v", got)
	}
	if !m.onList() {
		t.Fatalf("dialog should close on the matching completion, screen = %v", m.screenID())
	}
}

func TestRenameCompletionDoesNotCloseResubmittedDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "before", t.TempDir(), "")
	m.selectSessionRow(t, "before")
	m.openRename()
	openRename(m).SetName("after")
	_, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first dispatch returned no command")
	}
	// Same dialog, same name; resubmit without reopening (e.g. after
	// cycling the tool). gens.dialog advanced, so cmd1's completion is stale.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.effects.Main.Pending) == 0 {
		t.Fatal("resubmit did not enqueue a job")
	}

	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if !renameOpen(m) {
		t.Fatalf("older completion closed the resubmitted dialog, screen = %v", m.screenID())
	}
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("matching completion did not close the dialog, screen = %v", m.screenID())
	}
	if names := sessionNames(m); len(names) != 1 || names[0] != "after" {
		t.Fatalf("rename not applied: %v", names)
	}
}

func TestRenameCompletionDoesNotCloseReopenedSameTargetDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("one", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "one")
	m.openRename()
	openRename(m).SetName("renamed")
	_, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd1 == nil {
		t.Fatal("first dispatch returned no command")
	}
	// Reopen the same group; the old completion must not close this fresh
	// dialog. The reopened rename targets a new name because the first one
	// already committed "renamed".
	m.base = baseList
	m.clearSurfaces()
	m.selectGroupRow(t, "one")
	m.openRename()
	if openRename(m).Path() != "one" {
		t.Fatalf("reopened wrong target: %q", openRename(m).Path())
	}
	openRename(m).SetName("renamed-again")

	updated, _ := m.Update(cmd1())
	*m = *updated.(*Model)
	if !renameOpen(m) {
		t.Fatalf("older completion closed the reopened dialog, screen = %v", m.screenID())
	}
	if openRename(m).Name() != "renamed-again" {
		t.Fatalf("older completion clobbered the reopened input: %q", openRename(m).Name())
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("matching completion did not close the dialog, screen = %v", m.screenID())
	}
}

func TestRenameCompletionFencesStalePolls(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "kid", dir, "old/inner")

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "old" {
			setRailCursor(m, i)
		}
	}
	m.openRename()
	openRename(m).SetName("fresh")
	openRename(m).SetDir(dir)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("group rename dispatch returned no command")
	}

	// A poll pass that read the list before the rename committed; its
	// listedAt predates the completion, so it must be fenced out.
	listedAt := time.Now().Add(-time.Second)
	staleSessions := []store.Session{{ID: "kid", Name: "kid", Group: "old/inner", Tool: "claude", Cwd: dir}}
	staleGroups := []string{"old", "old/inner"}
	stalePaths := map[string]string{"old": dir}

	m.applyCmd(t, cmd)
	if m.effects.latestObservation.IsZero() {
		t.Fatal("rename completion did not advance the observation fence")
	}

	m.Update(refreshMsg{sessions: staleSessions, listedAt: listedAt, groups: staleGroups, groupPaths: stalePaths})
	for _, g := range m.workspace.groups {
		if strings.HasPrefix(g, "old") {
			t.Fatalf("stale poll restored the pre-rename group: %v", m.workspace.groups)
		}
	}
	if !hasWorkspaceGroup(m, "fresh/inner") {
		t.Fatalf("committed rename missing from workspace after stale poll: %v", m.workspace.groups)
	}
}

func hasWorkspaceGroup(m *Model, path string) bool {
	for _, g := range m.workspace.groups {
		if g == path {
			return true
		}
	}
	return false
}

func TestMoveSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("target/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if !moveOpen(m) {
		t.Fatal("openMove should enter move mode")
	}
	pickGroup(t, m, "target/deep")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "target/deep" {
		t.Fatalf("move failed: %+v", sessions)
	}
}

func TestMoveGroupRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.services.store.CreateGroup("beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "alpha/inner")

	m.selectGroupRow(t, "alpha/inner")
	m.openMove()
	if !moveOpen(m) {
		t.Fatal("openMove on a group row should enter move mode")
	}
	for _, opt := range picker(m).Groups {
		if opt.Path == "alpha/inner" {
			t.Fatal("picker offers the moved group itself")
		}
	}
	pickGroup(t, m, "beta")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "beta/inner" {
		t.Fatalf("group move failed: %+v", sessions)
	}
	if !m.onList() {
		t.Fatal("should return to list mode")
	}
}

func TestMoveGroupRowExcludesDescendants(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("alpha/inner/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "alpha")
	m.openMove()
	for _, opt := range picker(m).Groups {
		if opt.Path == "alpha" || opt.Path == "alpha/inner" || opt.Path == "alpha/inner/deep" {
			t.Fatalf("picker offers %q inside the moved subtree", opt.Path)
		}
	}
}

func TestMoveTerminalOntoAgentNests(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	agent, _ := m.services.store.Get(m.sessionRows()[0].ID)
	for i, opt := range picker(m).Groups {
		if opt.SessionID == agent.ID {
			picker(m).GroupIndex = i
			break
		}
	}
	if picker(m).Groups[picker(m).GroupIndex].SessionID != agent.ID {
		t.Fatal("picker must list the agent")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("nested = %+v err %v", got, err)
	}
}

func TestMoveTerminalOntoGroupUnnests(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.CreateGroup("other", dir); err != nil {
		t.Fatalf("other: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	pickGroup(t, m, "other")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, _ := m.services.store.Get(shell.ID)
	if got.ParentID != "" || got.Group != "other" {
		t.Fatalf("unnest = %+v", got)
	}
}

func TestMoveReportsPlacementFailure(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	agent := m.sessionRows()[0]
	shell := spawnTerminal(t, m)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	for picker(m).Groups[picker(m).GroupIndex].SessionID != agent.ID {
		picker(m).GroupIndex++
		if picker(m).GroupIndex >= len(picker(m).Groups) {
			t.Fatal("picker must list the agent")
		}
	}
	if err := m.services.store.Delete(agent.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("failed placement reported nothing")
	}
	if !moveOpen(m) {
		t.Fatalf("screen = %v, want the move card", m.screenID())
	}
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("shell left its parent: %+v err %v", got, err)
	}
}

func TestMoveReportsMissingSource(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	if err := m.services.store.Delete(shell.ID); err != nil {
		t.Fatalf("delete shell: %v", err)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("missing source reported nothing")
	}
	if !moveOpen(m) {
		t.Fatalf("screen = %v, want the move card", m.screenID())
	}
}

func TestMovePickerKeepsTheTerminalsOwnGroupSelected(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"alpha", "beta"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatalf("group %s: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "beta")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	if m.selectedGroupPath() != "beta" {
		t.Fatalf("selected group = %q, want beta", m.selectedGroupPath())
	}
	if sessID := picker(m).Groups[picker(m).GroupIndex].SessionID; sessID != "" {
		t.Fatalf("selected option targets session %q", sessID)
	}
}

func TestMoveAgentPickerHasNoSessionTargets(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	m.openMove()
	for _, opt := range picker(m).Groups {
		if opt.SessionID != "" {
			t.Fatalf("agent move listed session %q", opt.SessionID)
		}
	}
}

func TestMoveCompletionDoesNotCloseReopenedSameTargetDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	moved := movingSession(m)
	pickGroup(t, m, "target")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move returned no effect command")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.openMove()
	m.applyCmd(t, cmd)
	if !moveOpen(m) {
		t.Fatal("older move completion closed the reopened dialog")
	}
	got, err := m.services.store.Get(moved)
	if err != nil || got.Group != "target" {
		t.Fatalf("accepted move did not reconcile: %+v, %v", got, err)
	}
}
