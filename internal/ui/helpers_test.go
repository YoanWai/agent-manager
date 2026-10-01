package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildModel(t *testing.T) *Model {
	t.Helper()
	return buildModelWithStorePath(t, filepath.Join(t.TempDir(), "state.db"))
}

func buildModelWithStorePath(t *testing.T, dbPath string) *Model {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	cfg := config.Config{
		SessionKeys: keybind.DefaultSession(),
		ListKeys:    keybind.DefaultList(),
		Tools: map[string]config.Tool{
			"claude": {Command: "cat", DefaultStatus: status.Idle},
			// Parks the terminal cursor below its footer and paints the
			// composer's caret itself, so the arrow step reads the
			// placeholder instead of the cursor cell.
			"command-code": {
				Command:             "cat",
				DefaultStatus:       status.Idle,
				ActivityCutoff:      "(?m)^❯",
				ComposerPlaceholder: "Ask your question...",
			},
			"claude-hooked": {
				Command:        `sh -c 'exec cat' --`,
				StatusSource:   "claude-hooks",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
				TurnEnd:        `^[✻✳✶✽✢·✦✧+*] \S+ for \d.*$`,
				BusyLine:       `^[✻✳✶✽✢·✦✧+*] Waiting for \d+ background agents? to finish`,
				LimitLine:      `(?m)You've hit your .+limit`,
				Rules: []config.Rule{
					{State: status.Waiting, Pattern: "Enter to confirm"},
					{State: status.Errored, Pattern: `(?im)^\s*error:`},
				},
			},
			// The terminal tab, carrying no command and the shell flag, the
			// way the generated config ships it.
			"terminal": {
				Shell:         true,
				DefaultStatus: status.Idle,
				InputPrefix:   `(?m)^\s*(?:\S+\s+){0,3}[❯>$#›»→%➜]\s`,
			},
			// Shows every control character it is sent as ^X, with the tty
			// flow and line-editing keys turned off so ctrl+q and ctrl+r
			// reach it as bytes rather than as XON or a reprint.
			"control-echo": {
				Command:       "sh -c 'stty -ixon -iexten; exec cat -v'",
				DefaultStatus: status.Idle,
			},
			"quietchat": {
				Command:        "cat",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^›",
			},
			// Composes on a bare blank row between rules, marking the caret
			// cell with a reverse-video space, the way pi does;
			// input_prefix declares that markerless line.
			"pi-tool": {
				Command:        "cat",
				DefaultStatus:  status.Idle,
				ActivityCutoff: `(?ms)\A.*^─{8,}[ \t]*$`,
				InputPrefix:    "^",
			},
			// Draws a fresh prompt for every line it reads, so the prompt
			// stays on the pane however long an inbox envelope is. Left to
			// `cat`, a long envelope scrolls it off and TypingHold never
			// clears.
			"ready-tool": {
				Command:        `sh -c 'printf "❯ "; while IFS= read -r line; do printf "\n❯ "; done'`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			"send-tool": {
				Command:        `sh -c 'printf "❯ "; while IFS= read -r line; do printf "\n❯ "; done'`,
				PromptMode:     "send",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			// Draws its input line at once and takes the prompt it launched
			// with half a second later, the way an agent finishes booting.
			"slow-take-tool": {
				Command:        `sh -c 'printf "❯ "; sleep 0.5; printf "\n❯ %s\n❯ " "$0"; cat'`,
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
	st, err := store.Open(dbPath)
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
	m.startup.booting = false
	t.Cleanup(func() {
		for _, s := range m.workspace.sessions {
			driver.Kill(s.ID)
		}
	})
	return m
}

func (m *Model) applyCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		m.drainEffects(t)
		cmd = m.refreshCmd()
	}
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
}

// drainEffects drives only finite UI effects; recurring preview/poll timers stay idle.
func (m *Model) drainEffects(t *testing.T) {
	t.Helper()
	for count := 0; count < 100; count++ {
		if m.effects.active == nil {
			m.nextEffectCmd()
		}
		if m.effects.active == nil {
			return
		}
		m.applyTestMsg(t, m.effects.active.command())
	}
	t.Fatal("effect queue did not become idle")
}
func (m *Model) applyTestMsg(t *testing.T, msg tea.Msg) {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			if cmd != nil {
				m.applyTestMsg(t, cmd())
			}
		}
		return
	}
	if msg == nil {
		return
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
}

// applyCmd drops whatever command a message produces, so a test whose next
// step is that command steps through this instead.
func (m *Model) stepCmd(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	updated, next := m.Update(msg)
	*m = *updated.(*Model)
	return next
}

// clearRequestOnCleanup drops the server-global detach marker when the test
// ends: one left behind reaches every later test that reads it.
func clearRequestOnCleanup(t *testing.T, m *Model) {
	t.Helper()
	t.Cleanup(func() {
		if err := m.services.tmux.ClearRequest(); err != nil {
			t.Errorf("ClearRequest: %v", err)
		}
	})
}

func (m *Model) sessionRows() []store.Session {
	var sessions []store.Session
	for _, r := range railRows(m) {
		if !r.isGroup {
			sessions = append(sessions, r.sess)
		}
	}
	return sessions
}

func (m *Model) selectSessionRow(t *testing.T, name string) {
	t.Helper()
	for i, r := range railRows(m) {
		if !r.isGroup && r.sess.Name == name {
			setRailCursor(m, i)
			return
		}
	}
	t.Fatalf("no session row named %q", name)
}

func (m *Model) selectGroupRow(t *testing.T, path string) {
	t.Helper()
	for i, r := range railRows(m) {
		if r.isGroup && r.group == path {
			setRailCursor(m, i)
			return
		}
	}
	t.Fatalf("no group row for %q", path)
}

// groupRowPaths lists the stored groups the tree paints, skipping root.
func (m *Model) groupRowPaths() []string {
	var paths []string
	for _, r := range railRows(m) {
		if r.isGroup && !r.isRoot() {
			paths = append(paths, r.group)
		}
	}
	return paths
}

func loadStoredRows(t *testing.T, m *Model) {
	t.Helper()
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	m.workspace.sessions = sessions
	m.workspace.groups = make([]string, len(groups))
	m.workspace.groupPaths = make(map[string]string, len(groups))
	m.workspace.archivedGroups = make(map[string]bool, len(groups))
	for i, group := range groups {
		m.workspace.groups[i] = group.Name
		m.workspace.groupPaths[group.Name] = group.Path
		if group.Archived {
			m.workspace.archivedGroups[group.Name] = true
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
		if opt.path == path && opt.sessID == "" {
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
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

// seedRepo builds a committed repo the worktree tests can branch from.
// It sits one level inside the temp directory so the sibling
// "<name>-worktrees" tree is cleaned up with it.
func seedRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"add", "."},
		{"commit", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func createWorktreeSession(t *testing.T, m *Model, name, repo string) store.Session {
	t.Helper()
	m.openForm()
	m.form.name.SetValue(name)
	m.form.dir.SetValue(repo)
	m.form.toolIndex = 0
	m.form.worktree = true
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	for _, sess := range m.workspace.sessions {
		if sess.Name == name {
			if sess.WorktreeBranch == "" {
				t.Fatalf("session %q did not spawn in a worktree: %+v", name, sess)
			}
			return sess
		}
	}
	t.Fatalf("session %q missing after spawn", name)
	return store.Session{}
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

func sessionNames(m *Model) []string {
	var names []string
	for _, sess := range m.sessionRows() {
		names = append(names, sess.Name)
	}
	return names
}

// waitForAgent waits for a session's pane to hold an agent, or to be left
// with only its shell.
func waitForAgent(t *testing.T, m *Model, sessID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if running, err := sessioncmd.AgentRunning(m.services.tmux, sessID); err == nil && running == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane, _ := m.services.tmux.CapturePane(sessID)
	t.Fatalf("pane never settled on agent running = %v; pane:\n%s", want, pane)
}

// createSessionOn spawns a session on a named tool, for the tests that
// care which CLI the pane is running.
func createSessionOn(t *testing.T, m *Model, name, tool, dir string) {
	t.Helper()
	m.openForm()
	m.form.name.SetValue(name)
	m.form.dir.SetValue(dir)
	picked := false
	for i, candidate := range sortedToolNames(m.services.cfg) {
		if candidate == tool {
			m.form.toolIndex, picked = i, true
		}
	}
	if !picked {
		t.Fatalf("no tool named %q in the picker", tool)
	}
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

// spawnSession is the synchronous fixture entry point for tests that need a
// session without going through a dialog. It drives the same worker and
// completion the deferred form/quick spawn does, so both paths stay
// reconciled.
func (m *Model) spawnSession(toolName, name, dir, group, prompt string, autoNamed, worktree bool) error {
	paneW, paneH := m.paneTargetSize()
	request := spawnRequest{
		kind:         spawnForm,
		toolName:     toolName,
		name:         name,
		dir:          dir,
		group:        group,
		prompt:       prompt,
		autoNamed:    autoNamed,
		worktree:     worktree,
		pickWorktree: worktree,
		pane:         sessioncmd.PaneSize{Width: paneW, Height: paneH},
	}
	services := effectServices{
		store: m.services.store, driver: m.services.tmux, gitDrv: m.services.gitDrv, cfg: m.services.cfg,
		watch: m.focusRuntime.watch,
	}
	if m.poller != nil {
		services.runner = m.poller.runner
	}
	if m.services.lifecycle != nil {
		services.lifecycle = m.services.lifecycle.Capture(m.services.cfg, m.services.setSnapshot)
	}
	result, err := services.runSpawn(request)
	m.applySpawnEffect(request, result.(spawnEffectResult), err)
	return err
}

// waitForPaneChild waits for a pane to run a named program, which is a
// step past waitForAgent: a shell counts as busy from the moment it forks,
// before the child has exec'd the program it was asked for.
func waitForPaneChild(t *testing.T, m *Model, sessID, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var children []string
	for time.Now().Before(deadline) {
		if pid, err := m.services.tmux.PanePID(sessID); err == nil {
			children = sysstat.Trees([]int{pid})[pid].Children
			for _, child := range children {
				if filepath.Base(child) == name {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane never ran %s; children = %v", name, children)
}

// quitAgent ends the CLI the session launched with, leaving the pane on the
// shell underneath it: the user pressing ctrl-d in their agent.
func quitAgent(t *testing.T, m *Model, sessID string) {
	t.Helper()
	waitForAgent(t, m, sessID, true)
	if err := m.services.tmux.SendKeys(sessID, "C-d"); err != nil {
		t.Fatalf("send ctrl-d: %v", err)
	}
	waitForAgent(t, m, sessID, false)
}

// useSessionKeys swaps the session key table the model and its driver
// read, the way a config.toml with a [keybindings.session] block would.
func useSessionKeys(t *testing.T, m *Model, detach, review, editor []string) {
	t.Helper()
	m.services.keys = sessionOf(t, detach, review, editor)
	m.services.tmux.SetSessionKeys(m.services.keys)
}

func sessionOf(t *testing.T, detach, review, editor []string) keybind.Table {
	t.Helper()
	return keybind.DefaultSession().
		With(keybind.Detach, bindingOf(t, detach...)).
		With(keybind.Review, bindingOf(t, review...)).
		With(keybind.Editor, bindingOf(t, editor...))
}

func bindingOf(t *testing.T, specs ...string) keybind.Binding {
	t.Helper()
	keys := make([]keybind.Key, 0, len(specs))
	for _, spec := range specs {
		key, err := keybind.Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}

func railMouse(t *testing.T, m *Model, name string, action tea.MouseAction, button tea.MouseButton) *Model {
	t.Helper()
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, name)[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + line, Action: action, Button: button})
	m = updated.(*Model)
	m.drainEffects(t)
	return m
}

func sessionRow(t *testing.T, m *Model, name string) treeRow {
	t.Helper()
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.Name == name {
			return row
		}
	}
	t.Fatalf("no row %s", name)
	return treeRow{}
}

func resetExecution(m *Model) {
	m.poller.runner = execution.New(m.poller.dependencies, m.poller.options)
}

func shotModel() *Model {
	now := time.Now()
	sess := func(name, group, tool, st string, age time.Duration) store.Session {
		return store.Session{
			ID: name, Name: name, Group: group, Tool: tool, Status: st,
			Cwd: "/Users/someone/dev/api", CreatedAt: now.Add(-24 * time.Hour),
			LastStatusAt: now.Add(-age),
		}
	}
	sessions := []store.Session{
		sess("db-migrations", "", "opencode", status.Waiting, 3*time.Minute),
		sess("notes", "", "grok", status.Idle, 12*time.Minute),
		sess("auth-refresh", "backend", "claude", status.Finished, 2*time.Minute),
		sess("add-rate-limiting", "backend", "claude", status.Working, 41*time.Second),
		sess("ui-polish", "backend/web", "codex", status.Working, 6*time.Minute),
		sess("flaky-e2e", "backend/web", "claude", status.Errored, 22*time.Minute),
	}
	rows := []treeRow{
		{sess: sessions[0]},
		{sess: sessions[1]},
		{isGroup: true, group: "backend"},
		{depth: 1, sess: sessions[2]},
		{depth: 1, sess: sessions[3]},
		{isGroup: true, group: "backend/web", depth: 1},
		{depth: 2, sess: sessions[4]},
		{depth: 2, sess: sessions[5]},
	}
	m := &Model{
		width:  120,
		height: 34,
		mode:   modeList,
		split:  splitState{ratio: defaultSplitRatio},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		workspace: workspace{
			sessions:   sessions,
			groupPaths: map[string]string{"backend": "/Users/someone/dev/api"},
			agents:     agentStats{count: 4, cpu: 12, ram: 9, rss: 1_530_000_000},
			net:        netStats{rates: true, down: 9_400_000, up: 2_100_000},
			snap: sysstat.Snapshot{
				CPUOK: true, CPUPercent: 22,
				MemOK: true, MemPercent: 75, MemUsed: 12_100_000_000, MemTotal: 16_000_000_000,
				SwapOK: true, SwapPercent: 43, SwapUsed: 4_500_000_000, SwapTotal: 8_000_000_000,
				DiskOK: true, DiskPercent: 88, DiskUsed: 400_000_000_000, DiskFree: 100_000_000_000, DiskTotal: 500_000_000_000,
				CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55,
			},
			preview: previewSample,
			proc:    sysstat.ProcStat{OK: true, CPUPercent: 4.2, RamPercent: 3.6, RSS: 612_000_000},
			procFor: "add-rate-limiting",
		},
		rail: railModelFromRows(rows, 4),
	}
	return m
}

const previewSample = "\x1b[38;5;110m◆\x1b[0m claude \x1b[38;5;240m·\x1b[0m add-rate-limiting\n" +
	"\n" +
	"\x1b[38;5;250m❯ Add a token bucket limiter to the public API\x1b[0m\n" +
	"\n" +
	"\x1b[38;5;240m●\x1b[0m Read(internal/api/router.go)\n" +
	"  \x1b[38;5;240m└\x1b[0m 214 lines\n" +
	"\n" +
	"\x1b[38;5;240m●\x1b[0m Edit(internal/api/limiter.go)\n" +
	"  \x1b[38;5;240m└\x1b[0m +48 −3\n" +
	"\n" +
	"\x1b[38;5;214m✳\x1b[0m Running tests… (14s · esc to interrupt)\n"

// foregroundTestCmd separates finite background effect completions from the
// editor/terminal command a wiring test is inspecting. It never runs ExecMsg.
func (m *Model) foregroundTestCmd(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	var messages []tea.Msg
	var collect func(tea.Msg)
	collect = func(msg tea.Msg) {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				collect(child())
			}
			return
		}
		if _, ok := msg.(effectCompletedMsg); ok {
			updated, next := m.Update(msg)
			*m = *updated.(*Model)
			if next != nil {
				collect(next())
			}
			return
		}
		if msg != nil {
			messages = append(messages, msg)
		}
	}
	if cmd != nil {
		collect(cmd())
	}
	m.drainEffects(t)
	if len(messages) == 0 {
		return nil
	}
	if len(messages) > 1 {
		t.Fatalf("expected one foreground message, got %d", len(messages))
	}
	return func() tea.Msg { return messages[0] }
}
