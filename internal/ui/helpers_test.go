package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// buildModel gives the test a model on a tmux server and store of its own,
// so a test that calls t.Parallel shares nothing with its neighbours but
// process state. A test stays serial when it touches that state: it swaps a
// package seam (lookPath, startEditor, captureClipboardImage, ...), sets the
// environment or the lipgloss color profile, changes the theme or opens
// Settings (the styles are package-wide), reads or writes the system
// clipboard, drives testSocket, or launches a session under a fixed id (its
// launch script sits in the shared temp dir under that id).
func buildModel(t *testing.T) *Model {
	t.Helper()
	return buildModelWithStorePath(t, filepath.Join(t.TempDir(), "state.db"))
}

// storeTemplate is the database a fresh store.Open leaves behind in an empty
// directory. Running every schema migration costs a test about half a second
// under -race, so openTestStore copies this file instead.
var storeTemplate []byte

func prepareStoreTemplate() (func(), error) {
	dir, err := os.MkdirTemp("", "amuitest-store-")
	if err != nil {
		return nil, fmt.Errorf("store template: %w", err)
	}
	remove := func() { os.RemoveAll(dir) }
	path := filepath.Join(dir, "state.db")
	st, err := store.Open(path)
	if err != nil {
		remove()
		return nil, fmt.Errorf("store template: %w", err)
	}
	if err := st.Close(); err != nil {
		remove()
		return nil, fmt.Errorf("store template: %w", err)
	}
	// A closed store has folded its WAL back into the file; one left
	// behind means the file alone is not the whole database.
	if _, err := os.Stat(path + "-wal"); err == nil {
		remove()
		return nil, fmt.Errorf("store template: %s-wal outlived Close", path)
	}
	if storeTemplate, err = os.ReadFile(path); err != nil {
		remove()
		return nil, fmt.Errorf("store template: %w", err)
	}
	return remove, nil
}

// openTestStore opens a store at path, seeding a path that holds nothing yet
// with the migrated template, which is what Open would have built there.
func openTestStore(t *testing.T, path string) *store.Store {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) && storeTemplate != nil {
		if err := os.WriteFile(path, storeTemplate, 0o600); err != nil {
			t.Fatalf("seed store: %v", err)
		}
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	return st
}

func buildModelWithStorePath(t *testing.T, dbPath string) *Model {
	t.Helper()
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	cfg := config.Config{
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
				InputPrefix:   `(?m)^\s*(?:(?:\S+\s+){0,3}[❯>$#›»→%➜]|PS [^\n>]*>)\s`,
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
			// Collapses a paste into a placeholder the way Claude Code does
			// and takes a second Enter to submit it, so the first one is
			// swallowed the way a busy composer swallows it. tmux writes a
			// paste a line at a time, so only a carriage return read after
			// the closing bracket counts as an Enter.
			"collapsing-tool": {
				Command: `sh -c 'stty raw -echo; printf "\033[?2004h❯ "; p=0; n=0; ` +
					`while :; do c=$(dd bs=65536 count=1 2>/dev/null | od -An -v -c | tr -d " \n"); ` +
					`case "$c" in *201~*) p=1; printf "[Pasted text #1 +6 lines]";; "\\r") [ $p = 1 ] && n=$((n+1));; esac; ` +
					`[ $n = 2 ] && { printf "\r\n❯ "; p=0; n=0; }; done'`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			// Collapses a paste into a placeholder and never submits it.
			"stuck-tool": {
				Command: `sh -c 'stty raw -echo; printf "❯ "; dd bs=65536 count=1 >/dev/null 2>&1; ` +
					`printf "[Pasted text #1 +6 lines]"; cat >/dev/null'`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			// Draws its input line at once and takes the prompt it launched
			// with a couple of seconds later, the way an agent finishes
			// booting; the take has to outlast a whole poll pass.
			"slow-take-tool": {
				Command:        `sh -c 'printf "❯ "; sleep 2; printf "\n❯ %s\n❯ " "$0"; cat'`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			// Stands in for the agent CLIs, which turn on mouse tracking and
			// scroll themselves instead of leaving history for tmux.
			"mouse-tool": {
				Command:       `sh -c 'printf "\033[?1003h\033[?1006h" && exec cat'`,
				DefaultStatus: status.Idle,
			},
			// Same claim on the mouse without asking for SGR, which is the
			// one case the reports have to fall back to the original
			// encoding.
			"x10-tool": {
				Command:       `sh -c 'printf "\033[?1003h" && exec cat'`,
				DefaultStatus: status.Idle,
			},
		},
	}
	// Windows has no cat or sh: the tools run the test binary's stand-ins.
	if runtime.GOOS == "windows" {
		for _, name := range testfixture.Tools() {
			tool, ok := cfg.Tools[name]
			if !ok {
				continue
			}
			tool.Command = testfixture.Command(name)
			cfg.Tools[name] = tool
		}
	}
	st := openTestStore(t, dbPath)
	t.Cleanup(func() { st.Close() })

	driver, err := tmux.NewWithSocket(testServer(t))
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	m, err := New(cfg, st, driver, engine, hooks.NewManager(t.TempDir()), "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.layout.width = 120
	m.layout.height = 40
	m.startup.booting = false
	t.Cleanup(func() {
		for _, s := range m.workspace.sessions {
			driver.Kill(s.ID)
		}
		panes, _ := driver.Panes()
		for id := range panes {
			driver.Kill(id)
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
		m.nextEffectCmd()
		job := m.effects.input.active
		if job == nil {
			job = m.effects.main.active
		}
		if job == nil {
			job = m.effects.remote.active
		}
		if job == nil {
			return
		}
		m.applyTestMsg(t, job.command())
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

// clearRequestOnCleanup drops the detach marker left on the server hosting
// session id when the test ends: one left behind reaches every later test
// that reads it.
func clearRequestOnCleanup(t *testing.T, m *Model, id string) {
	t.Helper()
	t.Cleanup(func() {
		if err := m.services.tmux.ClearRequest(id); err != nil {
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

// focusGroupField tabs the open group form to field.
func focusGroupField(t *testing.T, m *Model, field int) {
	t.Helper()
	for range 5 {
		if m.groupForm.Focus() == field {
			return
		}
		m.handleGroupFormKey(tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("group form focus = %d, want %d", m.groupForm.Focus(), field)
}

func createSession(t *testing.T, m *Model, name, dir, group string) {
	t.Helper()
	m.openForm()
	m.form.SetName(name)
	m.form.SetDir(dir)
	setFormTool(m, 0)
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
	m.form.SetName(name)
	m.form.SetDir(repo)
	setFormTool(m, 0)
	m.form.SeedWorktree(true)
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
	out, err := tmuxCmd(t, "display-message", "-p", "-t", "am_"+id, "#{window_width}").CombinedOutput()
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

// shellSettle is how long a pane must stay empty before waitForAgent takes
// it as left with only its shell. The agent exiting hands the pane to a
// fresh interactive shell, whose startup files can fork for a while; one
// empty sample taken before that lands says nothing about the samples a
// revive takes next, and AgentRunning counts those forks as an agent.
const shellSettle = 500 * time.Millisecond

// waitForAgent waits for a session's pane to hold an agent, or to be left
// with only its shell and stay that way for shellSettle.
func waitForAgent(t *testing.T, m *Model, sessID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var emptySince time.Time
	for time.Now().Before(deadline) {
		running, err := sessioncmd.AgentRunning(m.services.tmux, sessID)
		switch {
		case err != nil || running != want:
			emptySince = time.Time{}
		case want:
			return
		case emptySince.IsZero():
			emptySince = time.Now()
		case time.Since(emptySince) >= shellSettle:
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
	m.form.SetName(name)
	m.form.SetDir(dir)
	picked := false
	for i, candidate := range m.services.cfg.AgentToolNames() {
		if candidate == tool {
			setFormTool(m, i)
			picked = true
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
func (m *Model) spawnSession(toolName, name, dir, group, prompt string, autoNamed, worktree bool, choice config.Choice) error {
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
		choice:       choice,
		base:         m.groupBase(group),
	}
	services := effectServices{
		store: m.services.store, driver: m.services.tmux, gitDrv: m.services.gitDrv, cfg: m.services.cfg,
		watch: m.focus.runtime.watch,
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

// waitForPreview refreshes until the selected session's pane has drawn:
// psmux's PowerShell takes a moment to start, and a blank pane paints no
// box to click.
func waitForPreview(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !paneBooted(m.workspace.preview) {
		if time.Now().After(deadline) {
			t.Fatal("the selected pane never drew")
		}
		time.Sleep(50 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}
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

// reloadModel is the manager started again on the same store, the way a
// restart reads back what Settings saved.
func reloadModel(t *testing.T, m *Model) *Model {
	t.Helper()
	loaded, err := New(m.services.cfg, m.services.store, m.services.tmux, m.services.engine, m.services.hooks, "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loaded
}

// useSessionKeys swaps the session key table the model and its driver
// read, the way the keys Settings stored would.
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
		layout: layoutState{
			width:  120,
			height: 34,
			split:  splitState{ratio: defaultSplitRatio},
		},
		mode: modeList,
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
				DiskOK: true, DiskPercent: 88, DiskUsed: 400_000_000_000, DiskAvailable: 100_000_000_000, DiskTotal: 500_000_000_000,
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
