package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDefaultToolFallsBackWhenSettingStale(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.SetDefaultTool("deleted-tool"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	if got := m.form().Tool(); got != "claude" {
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
	m.groupSurface().SetName("based")
	m.groupSurface().SetDir(repo)
	focusGroupField(t, m, uidialogs.GroupFormBaseField)
	_, step := m.pressGroupKey(tea.KeyMsg{Type: tea.KeyRight})
	m.applyBaseStep(t, step)
	if m.groupSurface().Base() != "develop" {
		t.Fatalf("first step past auto = %q, want develop", m.groupSurface().Base())
	}
	if view := m.viewGroupForm(); !strings.Contains(view, "◂ develop ▸") {
		t.Fatalf("base row should show the pick, got %q", view)
	}
	_, back := m.pressGroupKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.applyBaseStep(t, back)
	if m.groupSurface().Base() != "" {
		t.Fatalf("stepping back = %q, want auto", m.groupSurface().Base())
	}
	_, again := m.pressGroupKey(tea.KeyMsg{Type: tea.KeyRight})
	m.applyBaseStep(t, again)
	_, cmd := m.pressGroupKey(tea.KeyMsg{Type: tea.KeyEnter})
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
	m.groupSurface().SetDir(t.TempDir())
	focusGroupField(t, m, uidialogs.GroupFormBaseField)
	_, step := m.pressGroupKey(tea.KeyMsg{Type: tea.KeyRight})
	m.applyBaseStep(t, step)
	if m.groupSurface().Base() != "" {
		t.Fatalf("a directory with no branches has nothing to pick, got %q", m.groupSurface().Base())
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
	m.form().SetDir(clone)
	// Routed past Update, whose own refresh would take the fetch this test
	// steps by hand.
	m.handleMsg(m.formWorktreeProbeCmd(false)())

	cmd := m.refreshSpawnBase()
	if cmd == nil {
		t.Fatal("an open form spawning a worktree should refresh its base")
	}
	next := m.recordBaseFetch(cmd().(uilaunch.BaseFetchedMsg))
	if view := m.viewForm(); !strings.Contains(view, "origin/main (auto) · fetching") {
		t.Fatalf("form should name the base while it fetches, got %q", view)
	}
	m.recordBaseFetch(next().(uilaunch.BaseFetchedMsg))
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
	for m.settingsCard().State().Field != uisettings.FieldBaseFetch {
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
	m.form().SetDir(clone)
	// Routed past Update, whose own refresh would take the fetch this test
	// steps by hand.
	m.handleMsg(m.formWorktreeProbeCmd(false)())
	if next := m.recordBaseFetch(m.refreshSpawnBase()().(uilaunch.BaseFetchedMsg)); next != nil {
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
	m.form().SetDir(clone)
	// Routed past Update, whose own refresh would take the fetch this test
	// steps by hand.
	m.handleMsg(m.formWorktreeProbeCmd(false)())
	next := m.recordBaseFetch(m.refreshSpawnBase()().(uilaunch.BaseFetchedMsg))
	m.recordBaseFetch(next().(uilaunch.BaseFetchedMsg))
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
	if got := m.form().Tool(); got != "ready-tool" {
		t.Fatalf("next form tool = %q, want last spawn", got)
	}
}

func TestFormCancelDoesNotRememberLastPick(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	m.form().SeedWorktree(true)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})

	m.applyTestMsg(t, m.openForm()())
	if got := m.form().Tool(); got != "claude" {
		t.Fatalf("cancelled pick must not seed the next form, got %q", got)
	}
	if m.form().Worktree() {
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
	m.form().SetName("after-install")
	m.form().SetDir(dir)
	if probe := m.toggleFormWorktree(); probe != nil {
		m.applyTestMsg(t, probe())
	}
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if !hintOpen(m) || hintFix(m).Retry == nil {
		t.Fatalf("expected a refused launch with retry, screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	if m.ledger.spawner.LastSpawnTool != "ready-tool" || m.ledger.spawner.LastSpawnWorktree {
		t.Fatalf("failed launch changed the last pick: %q, %v", m.ledger.spawner.LastSpawnTool, m.ledger.spawner.LastSpawnWorktree)
	}
	fix := hintFix(m)
	fix.Command, fix.Script = installCommand, installCommand
	m.presentLaunchHint(fix)
	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if m.ledger.spawner.LastSpawnTool != "claude" || !m.ledger.spawner.LastSpawnWorktree {
		t.Fatalf("successful retry did not remember the pick: %q, %v; err=%q", m.ledger.spawner.LastSpawnTool, m.ledger.spawner.LastSpawnWorktree, m.errBar.text)
	}
	m.applyTestMsg(t, m.openForm()())
	if m.form().Tool() != "claude" || !m.form().Worktree() {
		t.Fatal("next form should use the successful retry's tool and worktree")
	}
}

func TestFormRemembersLastSpawnWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form().SeedWorktree(true)
	submitFormSession(t, m, "first")

	m.applyTestMsg(t, m.openForm()())
	if !m.form().Worktree() {
		t.Fatal("next form should seed worktree from the last spawn")
	}
}

func TestFormLastWorktreeYieldsToGroupDefault(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form().SeedWorktree(true)
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
	if m.form().Worktree() {
		t.Fatal("a group's explicit worktree default must not follow the last pick")
	}

	if err := m.services.store.CreateGroup("grp/child", t.TempDir()); err != nil {
		t.Fatalf("child group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp/child")
	m.openForm()
	if m.form().Worktree() {
		t.Fatal("a child of an explicit-off group must not follow the last pick")
	}

	m.base = baseList
	m.clearSurfaces()
	m.selectSessionRow(t, "first")
	m.openForm()
	if !m.form().Worktree() {
		t.Fatal("an inheriting group should still follow the last pick")
	}
}

func TestFormHiddenLastToolFallsBackToSettings(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	m.form().SeedWorktree(true)
	submitFormSession(t, m, "first")
	if err := m.services.store.SetHiddenTools(map[string]bool{"ready-tool": true}); err != nil {
		t.Fatal(err)
	}

	m.applyTestMsg(t, m.openForm()())
	if got := m.form().Tool(); got != "claude" {
		t.Fatalf("hidden last tool should fall back to settings, got %q", got)
	}
	if m.form().Worktree() {
		t.Fatal("hidden last tool should fall back to the settings worktree default")
	}
}

func TestSettingsDefaultIgnoresLastSpawn(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	pickFormTool(t, m, "ready-tool")
	submitFormSession(t, m, "first")

	m.openSettings()
	if got := m.settingsCard().State().ToolNames[m.settingsCard().State().ToolIndex]; got != "claude" {
		t.Fatalf("settings default tool = %q, want stored default", got)
	}
}

func TestFormWorktreeToggleSeedsFromSetting(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	if m.form().Worktree() {
		t.Fatal("worktree should default off with no setting")
	}
	m.base = baseList
	m.clearSurfaces()
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	if !m.form().Worktree() {
		t.Fatal("worktree should seed on from setting")
	}
}

func TestFormWorktreeGatedInNonRepoDir(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	plain := t.TempDir()
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openForm()
	m.form().SetDir(plain)
	if m.formWorktreeOn() {
		t.Fatal("a non-repo dir cannot host a worktree, even with the setting on")
	}
	if view := m.viewForm(); !strings.Contains(view, uilaunch.WorktreeUnavailable) {
		t.Fatalf("form should mark worktree unavailable, got %q", view)
	}
	m.form().FocusField(m.spawnHost(), spawn.FieldWorktree)
	_, probe := m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if probe != nil {
		m.applyTestMsg(t, probe())
	}
	if m.formWorktreeOn() {
		t.Fatal("toggling must not turn worktree on for a non-repo dir")
	}
	if !strings.Contains(m.errBar.text, "need a git repository") {
		t.Fatalf("refused toggle should say why, got %q", m.errBar.text)
	}
	_, spawn := m.submitForm()
	m.applyCmd(t, spawn)
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("gated form should still launch a plain session, got %d", len(sessions))
	}
	if sessions[0].WorktreeRepo != "" {
		t.Fatalf("gated spawn must not record a worktree: %q", sessions[0].WorktreeRepo)
	}
}

func TestWorktreeCapabilityExpiresSoAFreshRepoIsSeen(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	root := t.TempDir()
	dir, seed := filepath.Join(root, "repo"), filepath.Join(root, "seed")
	for _, path := range []string{dir, seed} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// git init can outlast worktreeLookupTTL on Windows, so the repo is built
	// before the answer is cached and only moved into place after.
	initGitRepo(t, seed)
	m.openForm()
	m.form().SetDir(dir)
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if capable, known := m.cachedWorktreeCapability(dir); !known || capable {
		t.Fatalf("plain directory cache = capable %t known %t", capable, known)
	}
	if err := os.Rename(filepath.Join(seed, ".git"), filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	if capable, known := m.cachedWorktreeCapability(dir); !known || capable {
		t.Fatalf("fresh cache changed without a probe: capable %t known %t", capable, known)
	}
	answer := m.ledger.spawner.WorktreeRepos[dir]
	answer.At = answer.At.Add(-uilaunch.WorktreeLookupTTL)
	m.ledger.spawner.WorktreeRepos[dir] = answer
	if capable, known := m.cachedWorktreeCapability(dir); !known || capable {
		t.Fatal("expiration discarded the last observed non-repository verdict")
	}
	if _, fresh := m.freshWorktreeCapability(dir); fresh {
		t.Fatal("expired capability still appeared fresh for a toggle")
	}
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if capable, known := m.cachedWorktreeCapability(dir); !known || !capable {
		t.Fatalf("fresh probe cache = capable %t known %t", capable, known)
	}
}

type blockedRepoPreflight struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	capable bool
}

func (r *blockedRepoPreflight) Resolve(string, []string) (string, bool) { return "", false }

func (r *blockedRepoPreflight) RepoCapable(string) bool {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.capable
}

func TestWorktreeProbeDefersGitAndRejectsReopenedForm(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	dir := t.TempDir()
	m.form().SetDir(dir)
	reader := &blockedRepoPreflight{
		started: make(chan struct{}), release: make(chan struct{}), capable: true,
	}
	m.gens.worktreeProbe++
	request := uilaunch.ProbeRequest{
		Target: uilaunch.ProbeForm, Generation: m.formPromptFor().Gen,
		Sequence: m.gens.worktreeProbe, Dir: m.formSpawnDir(), Toggle: true, From: m.form().Worktree(),
	}
	cmd := uilaunch.ProbeCmd(request, reader)
	select {
	case <-reader.started:
		t.Fatal("repo probe ran on the update path")
	default:
	}
	completed := make(chan uilaunch.ProbeMsg, 1)
	go func() { completed <- cmd().(uilaunch.ProbeMsg) }()
	<-reader.started
	m.openForm()
	close(reader.release)
	m.handleWorktreeProbe(<-completed)
	if m.form().Worktree() {
		t.Fatal("old repo probe toggled the reopened form")
	}
	if _, known := m.cachedWorktreeCapability(dir); known {
		t.Fatal("stale repo probe populated rendering facts")
	}
}

func TestPrepareFrameConsumesCapturedWorktreeFacts(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.ledger.spawner.WorktreeRepos = nil
	m.prepareFrame()
	if m.ledger.spawner.WorktreeRepos != nil {
		t.Fatal("rendering performed a repository lookup")
	}
}

func TestFormWorktreeStaysOnInRepoDir(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	m.form().SetDir(repo)
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if !m.formWorktreeOn() {
		t.Fatal("a repo dir should keep the worktree default on")
	}
	m.form().FocusField(m.spawnHost(), spawn.FieldWorktree)
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formWorktreeOn() {
		t.Fatal("toggle should turn worktree off in a repo dir")
	}
}
