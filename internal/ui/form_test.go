package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
)

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
		t.Fatalf("group form should close, err=%q", m.errBar.text)
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

func TestFormRejectsDashLeadingPrompt(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("flagged")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	m.form.prompt.SetValue("--version")

	if _, _ = m.submitForm(); m.errBar.text == "" {
		t.Fatal("dash-leading prompt should be rejected")
	}
	if m.mode != modeForm {
		t.Fatalf("form should stay open, mode = %v", m.mode)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session should be created, got %d", len(m.sessionRows()))
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

	if err := m.spawnSession("claude", "claude-aaaa", dir, "", "/compact", true, false); err != nil {
		t.Fatalf("slash spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	slashID := m.sessionRows()[0].ID
	if !m.poller.directivePending(slashID) {
		t.Fatal("slash-prompt spawn should defer the directive")
	}

	if err := m.spawnSession("claude", "claude-bbbb", dir, "", "do things", true, false); err != nil {
		t.Fatalf("plain spawn: %v", err)
	}
	if err := m.spawnSession("claude", "custom", dir, "", "/compact", false, false); err != nil {
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
	if err := m.spawnSession("ready-tool", "ready-tool-abcd", t.TempDir(), "", "", true, false); err != nil {
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

func TestSortedToolNamesOrder(t *testing.T) {
	cfg := config.Config{Tools: map[string]config.Tool{
		"grok":     {Command: "grok"},
		"gemini":   {Command: "gemini"},
		"codex":    {Command: "codex"},
		"claude":   {Command: "claude"},
		"opencode": {Command: "opencode"},
		"zephyr":   {Command: "zephyr"},
		"acme":     {Command: "acme"},
	}}
	got := sortedToolNames(cfg)
	want := []string{"claude", "opencode", "codex", "grok", "gemini", "acme", "zephyr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedToolNames = %v want %v", got, want)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestFormWorktreeToggleSeedsFromSetting(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	if m.form.worktree {
		t.Fatal("worktree should default off with no setting")
	}
	m.mode = modeList
	if err := m.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openForm()
	if !m.form.worktree {
		t.Fatal("worktree should seed on from setting")
	}
}

func TestSpawnWorktreeSessionCreatesWorktree(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)

	if err := m.spawnSession("claude", "wt-feat", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	sess := sessions[0]
	wantCwd := filepath.Join(filepath.Dir(sess.WorktreeRepo), filepath.Base(sess.WorktreeRepo)+"-worktrees", "wt-feat")
	if sess.Cwd != wantCwd {
		t.Fatalf("cwd = %q, want %q", sess.Cwd, wantCwd)
	}
	if sess.WorktreeBranch != "am/wt-feat" {
		t.Fatalf("branch = %q", sess.WorktreeBranch)
	}
	if _, err := os.Stat(sess.Cwd); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
}

func TestSpawnWorktreeInNonRepoBlocks(t *testing.T) {
	m := buildModel(t)
	plain := t.TempDir()
	err := m.spawnSession("claude", "wt-fail", plain, "", "", false, true)
	if err == nil {
		t.Fatal("non-repo dir must block the spawn")
	}
	sessions, listErr := m.store.ListSessions(true)
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(sessions) != 0 {
		t.Fatal("no session row should exist after a blocked spawn")
	}
}
