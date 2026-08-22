package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func buildModel(t *testing.T) *Model {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	cfg := config.Config{
		Tools: map[string]config.Tool{
			"claude": {Command: "cat", DefaultStatus: status.Idle},
			"claude-hooked": {
				Command:        "cat",
				StatusSource:   "claude-hooks",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
				TurnEnd:        `^[✻✳✶✽✢·✦✧+*] \S+ for \d.*$`,
				BusyLine:       `^[✻✳✶✽✢·✦✧+*] (?:Waiting for \d+ background agents? to finish|.*· \d+ shells? still running)`,
				LimitLine:      `(?m)You've hit your .+limit`,
				Rules: []config.Rule{
					{State: status.Waiting, Pattern: "Enter to confirm"},
					{State: status.Errored, Pattern: `(?im)^\s*error:`},
				},
			},
			// The terminal tab, carrying no command and the shell flag, the
			// way the generated config ships it.
			"terminal": {Shell: true, DefaultStatus: status.Idle},
			"quietchat": {
				Command:        "cat",
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^›",
			},
			"ready-tool": {
				Command:        `printf '❯ ' && cat`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
			"send-tool": {
				Command:        `printf '❯ ' && cat`,
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
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
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
	t.Cleanup(func() {
		for _, s := range m.sessions {
			driver.Kill(s.ID)
		}
	})
	return m
}

func (m *Model) applyCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		// Actions poke the background poller instead of returning a
		// command; tests run the equivalent refresh synchronously.
		cmd = m.refreshCmd()
	}
	msg := cmd()
	if msg == nil {
		return
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
}

// clearRequestOnCleanup drops the server-global detach marker when the test
// ends: one left behind reaches every later test that reads it.
func clearRequestOnCleanup(t *testing.T, m *Model) {
	t.Helper()
	t.Cleanup(func() {
		if err := m.tmux.ClearRequest(); err != nil {
			t.Errorf("ClearRequest: %v", err)
		}
	})
}

func (m *Model) sessionRows() []store.Session {
	var sessions []store.Session
	for _, r := range m.rows {
		if !r.isGroup {
			sessions = append(sessions, r.sess)
		}
	}
	return sessions
}

func (m *Model) selectSessionRow(t *testing.T, name string) {
	t.Helper()
	for i, r := range m.rows {
		if !r.isGroup && r.sess.Name == name {
			m.cursor = i
			return
		}
	}
	t.Fatalf("no session row named %q", name)
}

func (m *Model) selectGroupRow(t *testing.T, path string) {
	t.Helper()
	for i, r := range m.rows {
		if r.isGroup && r.group == path {
			m.cursor = i
			return
		}
	}
	t.Fatalf("no group row for %q", path)
}

// groupRowPaths lists the stored groups the tree paints, skipping root.
func (m *Model) groupRowPaths() []string {
	var paths []string
	for _, r := range m.rows {
		if r.isGroup && !r.isRoot() {
			paths = append(paths, r.group)
		}
	}
	return paths
}

func loadStoredRows(t *testing.T, m *Model) {
	t.Helper()
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	m.sessions = sessions
	m.groups = make([]string, len(groups))
	m.groupPaths = make(map[string]string, len(groups))
	m.archivedGroups = make(map[string]bool, len(groups))
	for i, group := range groups {
		m.groups[i] = group.Name
		m.groupPaths[group.Name] = group.Path
		if group.Archived {
			m.archivedGroups[group.Name] = true
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
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.applyCmd(t, cmd)
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
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.applyCmd(t, cmd)
	for _, sess := range m.sessions {
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
		if running, err := agentRunning(m.tmux, sessID); err == nil && running == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane, _ := m.tmux.CapturePane(sessID)
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
	for i, candidate := range sortedToolNames(m.cfg) {
		if candidate == tool {
			m.form.toolIndex, picked = i, true
		}
	}
	if !picked {
		t.Fatalf("no tool named %q in the picker", tool)
	}
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.applyCmd(t, cmd)
}

// waitForPaneChild waits for a pane to run a named program, which is a
// step past waitForAgent: a shell counts as busy from the moment it forks,
// before the child has exec'd the program it was asked for.
func waitForPaneChild(t *testing.T, m *Model, sessID, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var children []string
	for time.Now().Before(deadline) {
		if pid, err := m.tmux.PanePID(sessID); err == nil {
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
	if err := m.tmux.SendKeys(sessID, "C-d"); err != nil {
		t.Fatalf("send ctrl-d: %v", err)
	}
	waitForAgent(t, m, sessID, false)
}
