package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestDefaultToolFallsBackWhenSettingStale(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.SetDefaultTool("deleted-tool"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	if got := m.form.Tool(); got != "claude" {
		t.Fatalf("form default tool = %q want claude (alphabetical fallback)", got)
	}
}

// repoWithDevelop is a committed repo whose develop branch sits one commit
// past main.
func repoWithDevelop(t *testing.T) string {
	t.Helper()
	repo := seedRepo(t)
	runGit(t, repo, "checkout", "-q", "-b", "develop")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "develop work")
	runGit(t, repo, "checkout", "-q", "main")
	return repo
}

func TestGroupFormStepsBaseThroughTheRepoBranches(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := repoWithDevelop(t)
	m.openGroupForm()
	m.groupForm.SetName("based")
	m.groupForm.SetDir(repo)
	focusGroupField(t, m, uidialogs.GroupFormBaseField)
	_, step := m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyRight})
	m.applyTestMsg(t, step())
	if m.groupForm.base != "develop" {
		t.Fatalf("first step past auto = %q, want develop", m.groupForm.base)
	}
	if view := m.viewGroupForm(); !strings.Contains(view, "◂ develop ▸") {
		t.Fatalf("base row should show the pick, got %q", view)
	}
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.groupForm.base != "" {
		t.Fatalf("stepping back = %q, want auto", m.groupForm.base)
	}
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Base != "develop" {
		t.Fatalf("group form should store the base, got %+v", groups)
	}
}

func TestGroupFormShowsTheBaseAutoInherits(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("parent", t.TempDir()); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.SetGroupBase("parent", "develop"); err != nil {
		t.Fatalf("set base: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "parent")
	m.openGroupForm()
	if view := m.viewGroupForm(); !strings.Contains(view, "◂ auto ▸  develop from parent") {
		t.Fatalf("auto under a parent with a base should name it, got %q", view)
	}
}

func TestGroupFormBaseNeedsARepository(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.SetDir(t.TempDir())
	focusGroupField(t, m, uidialogs.GroupFormBaseField)
	_, step := m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyRight})
	m.applyTestMsg(t, step())
	if m.groupForm.base != "" {
		t.Fatalf("a directory with no branches has nothing to pick, got %q", m.groupForm.base)
	}
	if !strings.Contains(m.errBar.text, "group base") {
		t.Fatalf("refused step should say why, got %q", m.errBar.text)
	}
}

func TestSpawnWorktreeBranchesFromTheGroupBase(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := repoWithDevelop(t)
	if err := m.services.store.CreateGroup("parent", repo); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.CreateGroup("parent/child", ""); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.SetGroupBase("parent", "develop"); err != nil {
		t.Fatalf("set base: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	if err := m.spawnSession("claude", "on-develop", repo, "parent/child", "", false, true, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got, want := gitOutput(t, sessions[0].Cwd, "rev-parse", "HEAD"), gitOutput(t, repo, "rev-parse", "develop"); got != want {
		t.Fatalf("worktree starts at %s, want the inherited base develop at %s", got, want)
	}
}

func TestFormShowsAndFetchesTheWorktreeBase(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	remote := seedRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, remote, "clone", "-q", remote, clone)
	runGit(t, remote, "commit", "-q", "--allow-empty", "-m", "after the clone")
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	m.form.SetDir(clone)
	// Routed past Update, whose own refresh would take the fetch this test
	// steps by hand.
	m.handleMsg(m.formWorktreeProbeCmd(false)())

	cmd := m.refreshSpawnBase()
	if cmd == nil {
		t.Fatal("an open form spawning a worktree should refresh its base")
	}
	next := m.recordBaseFetch(cmd().(baseFetchedMsg))
	if view := m.viewForm(); !strings.Contains(view, "origin/main (auto) · fetching") {
		t.Fatalf("form should name the base while it fetches, got %q", view)
	}
	m.recordBaseFetch(next().(baseFetchedMsg))
	if got, want := gitOutput(t, clone, "rev-parse", "origin/main"), gitOutput(t, remote, "rev-parse", "HEAD"); got != want {
		t.Fatalf("origin/main = %s after the fetch, want %s", got, want)
	}
	// The repo answer lasts worktreeLookupTTL, which a slow fetch can
	// outlive; Update's refresh would ask again, so the test does too.
	m.handleMsg(m.formWorktreeProbeCmd(false)())
	if view := m.viewForm(); !strings.Contains(view, "origin/main (auto)") || strings.Contains(view, "fetching") {
		t.Fatalf("a finished fetch leaves the base alone on its row, got %q", view)
	}
	if m.refreshSpawnBase() != nil {
		t.Fatal("a base fetched a moment ago should not be fetched again")
	}
}

func TestSettingsFetchOnSpawnTurnsTheFetchOff(t *testing.T) {
	m := buildModel(t)
	remote := seedRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, remote, "clone", "-q", remote, clone)
	runGit(t, remote, "commit", "-q", "--allow-empty", "-m", "after the clone")
	cached := gitOutput(t, clone, "rev-parse", "origin/main")

	m.openSettings()
	for m.settings.dialog.Field != uisettings.FieldBaseFetch {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !strings.Contains(ansi.Strip(m.viewSettings()), "fetch on spawn") {
		t.Fatalf("settings do not show the fetch row:\n%s", ansi.Strip(m.viewSettings()))
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(uisettings.BaseFetchSetting); err != nil || chosen != "off" {
		t.Fatalf("want stored off, got %q err %v", chosen, err)
	}

	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	m.form.SetDir(clone)
	// Routed past Update, whose own refresh would take the fetch this test
	// steps by hand.
	m.handleMsg(m.formWorktreeProbeCmd(false)())
	if next := m.recordBaseFetch(m.refreshSpawnBase()().(baseFetchedMsg)); next != nil {
		t.Fatal("with the fetch turned off, resolving the base should be the last step")
	}
	if view := m.viewForm(); !strings.Contains(view, "origin/main (auto)") || strings.Contains(view, "fetch") {
		t.Fatalf("the base row should name the base alone, got %q", view)
	}
	if got := gitOutput(t, clone, "rev-parse", "origin/main"); got != cached {
		t.Fatalf("origin/main moved to %s with the fetch off, want %s", got, cached)
	}
}

func TestFormMarksAFailedBaseFetch(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	remote := seedRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, remote, "clone", "-q", remote, clone)
	runGit(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	m.form.SetDir(clone)
	// Routed past Update, whose own refresh would take the fetch this test
	// steps by hand.
	m.handleMsg(m.formWorktreeProbeCmd(false)())
	next := m.recordBaseFetch(m.refreshSpawnBase()().(baseFetchedMsg))
	m.recordBaseFetch(next().(baseFetchedMsg))
	if view := m.viewForm(); !strings.Contains(view, "origin/main (auto) · fetch failed") {
		t.Fatalf("form should say the base was not refreshed, got %q", view)
	}
}

func TestFormRemembersLastSpawnTool(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	submitFormSession(t, m, "first")

	m.openForm()
	if got := m.form.Tool(); got != "ready-tool" {
		t.Fatalf("next form tool = %q, want last spawn", got)
	}
}

func TestFormCancelDoesNotRememberLastPick(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	m.form.SeedWorktree(true)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})

	m.applyTestMsg(t, m.openForm()())
	if got := m.form.Tool(); got != "claude" {
		t.Fatalf("cancelled pick must not seed the next form, got %q", got)
	}
	if m.form.Worktree() {
		t.Fatal("cancelled worktree pick must not seed the next form")
	}
}

func TestFormRemembersPickOnlyAfterInstallRetrySucceeds(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	submitFormSession(t, m, "first")

	installCommand := fakeInstallCommand(t)
	tool := m.services.cfg.Tools["claude"]
	tool.Command = "am-fake-cli"
	m.services.cfg.Tools["claude"] = tool
	dir := t.TempDir()
	initGitRepo(t, dir)
	m.openForm()
	pickFormTool(t, m, "claude")
	m.form.SetName("after-install")
	m.form.SetDir(dir)
	if probe := m.toggleFormWorktree(); probe != nil {
		m.applyTestMsg(t, probe())
	}
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if m.mode != modeLaunchHint || m.launchHint.fix.effectRetry == nil {
		t.Fatalf("expected a refused launch with retry, mode=%v err=%q", m.mode, m.errBar.text)
	}
	if m.ledger.lastSpawnTool != "ready-tool" || m.ledger.lastSpawnWorktree {
		t.Fatalf("failed launch changed the last pick: %q, %v", m.ledger.lastSpawnTool, m.ledger.lastSpawnWorktree)
	}
	m.launchHint.fix.command = installCommand
	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if m.ledger.lastSpawnTool != "claude" || !m.ledger.lastSpawnWorktree {
		t.Fatalf("successful retry did not remember the pick: %q, %v; err=%q", m.ledger.lastSpawnTool, m.ledger.lastSpawnWorktree, m.errBar.text)
	}
	m.applyTestMsg(t, m.openForm()())
	if m.form.Tool() != "claude" || !m.form.Worktree() {
		t.Fatal("next form should use the successful retry's tool and worktree")
	}
}

func TestFormRemembersLastSpawnWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form.SeedWorktree(true)
	submitFormSession(t, m, "first")

	m.applyTestMsg(t, m.openForm()())
	if !m.form.Worktree() {
		t.Fatal("next form should seed worktree from the last spawn")
	}
}

func TestFormLastWorktreeYieldsToGroupDefault(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form.SeedWorktree(true)
	submitFormSession(t, m, "first")

	if err := m.services.store.CreateGroup("grp", t.TempDir()); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.SetGroupWorktree("grp", "off"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openForm()
	if m.form.Worktree() {
		t.Fatal("a group's explicit worktree default must not follow the last pick")
	}

	if err := m.services.store.CreateGroup("grp/child", t.TempDir()); err != nil {
		t.Fatalf("child group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp/child")
	m.openForm()
	if m.form.Worktree() {
		t.Fatal("a child of an explicit-off group must not follow the last pick")
	}

	m.mode = modeList
	m.selectSessionRow(t, "first")
	m.openForm()
	if !m.form.Worktree() {
		t.Fatal("an inheriting group should still follow the last pick")
	}
}

func TestFormHiddenLastToolFallsBackToSettings(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	m.form.SeedWorktree(true)
	submitFormSession(t, m, "first")
	if err := m.services.store.SetHiddenTools(map[string]bool{"ready-tool": true}); err != nil {
		t.Fatal(err)
	}

	m.applyTestMsg(t, m.openForm()())
	if got := m.form.Tool(); got != "claude" {
		t.Fatalf("hidden last tool should fall back to settings, got %q", got)
	}
	if m.form.Worktree() {
		t.Fatal("hidden last tool should fall back to the settings worktree default")
	}
}

func TestSettingsDefaultIgnoresLastSpawn(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	submitFormSession(t, m, "first")

	m.openSettings()
	if got := m.settings.dialog.ToolNames[m.settings.dialog.ToolIndex]; got != "claude" {
		t.Fatalf("settings default tool = %q, want stored default", got)
	}
}
