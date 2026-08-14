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
	"github.com/YoanWai/agent-manager/internal/launch"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNewSessionFormUsesSettingsDefaultTool(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting("default_tool", "ready-tool"); err != nil {
		t.Fatal(err)
	}
	m.openForm()
	if got := m.form.toolNames[m.form.toolIndex]; got != "ready-tool" {
		t.Fatalf("new session tool = %q, want settings default", got)
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

func TestGroupFormShowsNewEmptyGroupWithWorktreeOff(t *testing.T) {
	m := buildModel(t)
	m.hideEmptyGroups = true
	m.search = "does-not-match"
	m.showArchived = true
	m.statusFilter = statusFilterAttention
	m.openGroupForm()
	m.groupForm.name.SetValue("manual")
	m.groupForm.path.SetValue(t.TempDir())
	m.groupForm.worktreeIndex = groupWorktreeIndex("off")

	_, cmd := m.submitGroupForm()
	if m.hideEmptyGroups {
		t.Fatal("creating a group should reveal it when empty groups were hidden")
	}
	if m.search != "" || m.showArchived || m.statusFilter.active() {
		t.Fatalf("creation left list filters active: search=%q archived=%v statusFilter=%v",
			m.search, m.showArchived, m.statusFilter)
	}
	if got := m.groupRowPaths(); !reflect.DeepEqual(got, []string{"manual"}) {
		t.Fatalf("group rows before refresh = %v, want [manual]", got)
	}
	if row, ok := m.selectedRow(); !ok || !row.isGroup || row.group != "manual" {
		t.Fatalf("new group is not selected: %+v", row)
	}

	m.applyCmd(t, cmd)
	if got := m.groupRowPaths(); !reflect.DeepEqual(got, []string{"manual"}) {
		t.Fatalf("group rows after refresh = %v, want [manual]", got)
	}
	if got := m.groupWorktrees["manual"]; got != "off" {
		t.Fatalf("local worktree choice = %q, want off", got)
	}
}

func TestGroupFormExpandsParentToShowNewChild(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("projects", t.TempDir()); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "projects")
	m.collapsed["projects"] = true
	m.openGroupForm()
	m.groupForm.name.SetValue("api")

	_, _ = m.submitGroupForm()
	if m.collapsed["projects"] {
		t.Fatal("parent remained collapsed after creating a child")
	}
	if got := m.groupRowPaths(); !reflect.DeepEqual(got, []string{"projects", "projects/api"}) {
		t.Fatalf("group rows = %v, want parent and child", got)
	}
	if row, ok := m.selectedRow(); !ok || row.group != "projects/api" {
		t.Fatalf("new child is not selected: %+v", row)
	}
}

func TestGroupFormRejectsDuplicateWithoutChangingIt(t *testing.T) {
	m := buildModel(t)
	first := t.TempDir()
	second := t.TempDir()
	if err := m.store.AddGroup("backend", first, "on"); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.openGroupForm()
	pickGroup(t, m, "")
	m.groupForm.name.SetValue("backend")
	m.groupForm.path.SetValue(second)
	m.groupForm.worktreeIndex = groupWorktreeIndex("off")
	_, cmd := m.submitGroupForm()
	if cmd != nil || m.mode != modeGroupForm || !strings.Contains(m.errBar.text, "already exists") {
		t.Fatalf("duplicate submission succeeded: mode=%v err=%q", m.mode, m.errBar.text)
	}

	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Path != first || groups[0].Worktree != "on" {
		t.Fatalf("duplicate submission changed group: %+v", groups)
	}
}

func TestGroupParentPickerExcludesArchivedGroups(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"active", "archived", "archived/child"} {
		if err := m.store.CreateGroup(group, t.TempDir()); err != nil {
			t.Fatalf("create %s: %v", group, err)
		}
	}
	if err := m.store.SetGroupArchived("archived", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.openGroupForm()
	var options []string
	for _, option := range m.form.groups {
		options = append(options, option.path)
	}
	if !reflect.DeepEqual(options, []string{"", "active"}) {
		t.Fatalf("parent options = %v, want root and active", options)
	}
}

func TestGroupFormFieldsTrackCardWidth(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	want := m.formValueWidth() - 3
	if m.groupForm.name.Width != want || m.groupForm.path.Width != want {
		t.Fatalf("initial widths = name %d path %d, want %d", m.groupForm.name.Width, m.groupForm.path.Width, want)
	}

	m.Update(tea.WindowSizeMsg{Width: 72, Height: m.height})
	want = m.formValueWidth() - 3
	if m.groupForm.name.Width != want || m.groupForm.path.Width != want {
		t.Fatalf("resized widths = name %d path %d, want %d", m.groupForm.name.Width, m.groupForm.path.Width, want)
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

	command, _, err := m.buildLaunch("claude", tool, launch.WithPrompt(tool, tool.Command, "fix the bug"), "prompt01")
	if err != nil {
		t.Fatalf("buildLaunch: %v", err)
	}
	if !strings.HasPrefix(command, "cat 'fix the bug' --mcp-config '") || !strings.Contains(command, "--settings '") {
		t.Fatalf("command = %q", command)
	}
}

func TestFormLongDirKeepsCursorEndVisible(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.formFocus(2) // name -> tool -> dir
	m.form.dir.SetValue("/very/long/" + strings.Repeat("a", 80) + "/tail-end")
	m.form.dir.CursorEnd()
	view := ansi.Strip(m.viewForm())
	if !strings.Contains(view, "tail-end") {
		t.Fatal("dir field should scroll so the end of a long value stays visible")
	}
}

func TestFormLongPromptWrapsAcrossRows(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.formFocus(-2) // name -> group -> prompt
	m.form.prompt.SetValue(strings.Repeat("word ", 25) + "finale")
	view := ansi.Strip(m.viewForm())
	if !strings.Contains(view, "finale") {
		t.Fatal("long prompt should wrap onto more rows instead of being clipped")
	}
}

func TestTextareaRowsCountsExactMultipleWrap(t *testing.T) {
	in := promptField()
	in.SetWidth(12) // content width 10
	in.SetValue("1234567890\nx")
	if rows := textareaRows(in, 10, 5); rows != 3 {
		t.Fatalf("a line filling its row exactly adds a wrap row: want 3, got %d", rows)
	}
}

func TestFormGroupArrowsMoveFocusNotSelection(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("alpha", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.openForm()
	m.formFocus(-1) // wrap from name to group
	if m.form.focus != fieldGroup {
		t.Fatalf("focus = %v, want fieldGroup", m.form.focus)
	}

	initial := m.form.groupIndex
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form.groupIndex != initial {
		t.Fatalf("down must not change the group selection, index = %d, want %d", m.form.groupIndex, initial)
	}
	if m.form.focus == fieldGroup {
		t.Fatal("down should move focus off the group field")
	}

	m.form.focus = fieldGroup
	m.form.groupIndex = 0
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.form.groupIndex != 1 {
		t.Fatalf("right should cycle the group selection, index = %d", m.form.groupIndex)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.form.groupIndex != 0 {
		t.Fatalf("left should cycle back, index = %d", m.form.groupIndex)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.form.groupIndex != len(m.form.groups)-1 {
		t.Fatalf("left at the first group should wrap to the last, index = %d", m.form.groupIndex)
	}
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.form.groupIndex != 0 {
		t.Fatalf("right at the last group should wrap to the first, index = %d", m.form.groupIndex)
	}
}

func TestGroupFormParentArrowsMoveFocusNotSelection(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("alpha", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.openGroupForm()
	m.groupForm.focus = gfParent

	initial := m.form.groupIndex
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.form.groupIndex != initial {
		t.Fatalf("down must not change the parent selection, index = %d, want %d", m.form.groupIndex, initial)
	}
	if m.groupForm.focus == gfParent {
		t.Fatal("down should move focus off the parent field")
	}

	m.groupForm.focus = gfParent
	m.form.groupIndex = 0
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.form.groupIndex != 1 {
		t.Fatalf("right should cycle the parent selection, index = %d", m.form.groupIndex)
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

// Only a spawn that hands over the rename directive has a name to wait for;
// one the user named itself is already at its final name.
func TestSpawnAwaitsARenameOnlyWhenItAsksForOne(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.spawnSession("claude", "claude-aaaa", dir, "", "do things", true, false); err != nil {
		t.Fatalf("auto-named spawn: %v", err)
	}
	if err := m.spawnSession("claude", "custom", dir, "", "do things", false, false); err != nil {
		t.Fatalf("custom spawn: %v", err)
	}
	for _, sess := range m.sessions {
		awaiting := m.awaitingRename(sess)
		if sess.Name == "claude-aaaa" && !awaiting {
			t.Fatal("an auto-named spawn should wait for the name its agent picks")
		}
		if sess.Name == "custom" && awaiting {
			t.Fatal("a custom-named spawn was never asked to rename")
		}
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
	if !sessionHasPendingInput(t, m, slashID, launch.DeferredRenameDirective) {
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
		if sessionHasPendingInput(t, m, sess.ID, launch.DeferredRenameDirective) {
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
	for sessionHasPendingInput(t, m, sess.ID, launch.DeferredRenameDirective) {
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

func TestSendModePromptSurvivesPollerRestart(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("send-tool", "custom", t.TempDir(), "", "do the work", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]
	if !sessionHasPendingInput(t, m, sess.ID, "do the work") {
		t.Fatal("launch prompt was not persisted before delivery")
	}
	old := m.poller
	m.poller = newPoller(m.store, m.tmux, m.engine, m.hooks, m.gitDrv,
		old.statusSources, old.sessionStores, old.mcpStyles, old.interval)
	m.applyCmd(t, m.refreshCmd())
	deadline := time.Now().Add(5 * time.Second)
	for len(sessionPendingInputs(t, m, sess.ID)) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("startup prompt stayed queued after a poller restart and the input box appeared")
		}
		time.Sleep(100 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pane, "do the work") || !strings.Contains(pane, "This session is already named") {
		t.Fatalf("pane did not receive the launch prompt:\n%s", pane)
	}
}

func TestSendModeReconcilesAmbiguousDeliveryWithoutResending(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("send-tool", "custom", t.TempDir(), "", "do not resend", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]
	input := sessionPendingInputs(t, m, sess.ID)[0]
	claimed, err := m.store.ClaimPendingInput(sess.ID, input)
	if err != nil || !claimed {
		t.Fatalf("claim pending input = %v, %v", claimed, err)
	}
	old := m.poller
	m.poller = newPoller(m.store, m.tmux, m.engine, m.hooks, m.gitDrv,
		old.statusSources, old.sessionStores, old.mcpStyles, old.interval)
	msg := m.poller.refreshOnce()
	gotErr, ok := msg.(errMsg)
	if !ok || !strings.Contains(gotErr.err.Error(), "ambiguous pending input") {
		t.Fatalf("refresh result = %#v, want ambiguous-delivery error", msg)
	}
	if inputs := sessionPendingInputs(t, m, sess.ID); len(inputs) != 0 {
		t.Fatalf("ambiguous input was not reconciled: %q", inputs)
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, "do not resend") {
		t.Fatalf("ambiguous input was resent:\n%s", pane)
	}
}

func TestSendModeSurfacesSendFailureAndDoesNotRetry(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("send-tool", "custom", t.TempDir(), "", "cannot deliver", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatal(err)
	}
	sent, err := m.poller.maybeSendPendingInput(sess, "❯ ", true)
	if err == nil || !strings.Contains(err.Error(), "send pending input") || sent {
		t.Fatalf("send result = %v, %v", sent, err)
	}
	claimed, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed.PendingInputClaimed {
		t.Fatal("failed delivery was not left in an ambiguous durable state")
	}
	sent, err = m.poller.maybeSendPendingInput(claimed, "", false)
	if err == nil || !strings.Contains(err.Error(), "ambiguous pending input") || !sent {
		t.Fatalf("reconcile result = %v, %v", sent, err)
	}
	if inputs := sessionPendingInputs(t, m, sess.ID); len(inputs) != 0 {
		t.Fatalf("ambiguous failed delivery was retried: %q", inputs)
	}
}

func sessionPendingInputs(t *testing.T, m *Model, id string) []string {
	t.Helper()
	sess, err := m.store.Get(id)
	if err != nil {
		t.Fatalf("get session %s: %v", id, err)
	}
	return sess.PendingInputs
}

func sessionHasPendingInput(t *testing.T, m *Model, id, want string) bool {
	t.Helper()
	for _, input := range sessionPendingInputs(t, m, id) {
		if input == want || strings.Contains(input, want) {
			return true
		}
	}
	return false
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
		"pi":       {Command: "pi"},
		"zephyr":   {Command: "zephyr"},
		"acme":     {Command: "acme"},
	}}
	got := sortedToolNames(cfg)
	want := []string{"claude", "opencode", "codex", "grok", "gemini", "pi", "acme", "zephyr"}
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

func TestFormWorktreeGatedInNonRepoDir(t *testing.T) {
	m := buildModel(t)
	plain := t.TempDir()
	if err := m.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openForm()
	m.form.dir.SetValue(plain)
	if m.formWorktreeOn() {
		t.Fatal("a non-repo dir cannot host a worktree, even with the setting on")
	}
	if view := m.viewForm(); !strings.Contains(view, worktreeUnavailable) {
		t.Fatalf("form should mark worktree unavailable, got %q", view)
	}
	m.form.focus = fieldWorktree
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formWorktreeOn() {
		t.Fatal("toggling must not turn worktree on for a non-repo dir")
	}
	if !strings.Contains(m.errBar.text, "need a git repository") {
		t.Fatalf("refused toggle should say why, got %q", m.errBar.text)
	}
	m.submitForm()
	sessions, err := m.store.ListSessions(true)
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
	m := buildModel(t)
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if m.worktreeCapable(dir) {
		t.Fatal("a plain directory cannot host a worktree")
	}
	initGitRepo(t, dir)
	if m.worktreeCapable(dir) {
		t.Fatal("the memo should still answer from the look taken a moment ago")
	}
	answer := m.worktreeRepos[dir]
	answer.at = answer.at.Add(-worktreeLookupTTL)
	m.worktreeRepos[dir] = answer
	if !m.worktreeCapable(dir) {
		t.Fatal("an expired entry should be looked up again and see the new repo")
	}
}

func TestFormWorktreeStaysOnInRepoDir(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openForm()
	m.form.dir.SetValue(repo)
	if !m.formWorktreeOn() {
		t.Fatal("a repo dir should keep the worktree default on")
	}
	m.form.focus = fieldWorktree
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.formWorktreeOn() {
		t.Fatal("toggle should turn worktree off in a repo dir")
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

func TestSpawnWorktreeRollsBackWhenLaunchBuildFails(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	hooksDir := m.hooks.Dir()
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hooksDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hooksDir, 0o755) })

	err := m.spawnSession("claude", "wt-launchfail", repo, "", "", false, true)
	if err == nil {
		t.Fatal("launch-build failure must block the spawn")
	}
	worktreePath := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees", "wt-launchfail")
	if _, statErr := os.Stat(worktreePath); !os.IsNotExist(statErr) {
		t.Fatal("worktree must be rolled back when the launch command cannot be built")
	}
}

func TestFormWorktreeSeedsFromGroupDefault(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("grp", t.TempDir()); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openForm()
	if !m.form.worktree {
		t.Fatal("form should seed worktree on from the group default")
	}
	pickGroup(t, m, "")
	m.moveGroupCursor(0)
	if m.form.worktree {
		t.Fatal("moving to root should follow its default off")
	}
}

func TestGroupFormStoresWorktreeChoice(t *testing.T) {
	m := buildModel(t)
	m.openGroupForm()
	m.groupForm.name.SetValue("wtgrp")
	m.groupForm.path.SetValue(t.TempDir())
	m.groupForm.focus = gfWorktree
	m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Worktree != "on" {
		t.Fatalf("group form should store the worktree choice, got %+v", groups)
	}
}
