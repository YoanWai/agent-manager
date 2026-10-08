package execution

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSocket = "amexectest"

func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", testSocket}, args...)...)
}
func TestMain(m *testing.M) {
	tmuxCmd("kill-server").Run()
	if _, err := exec.LookPath("tmux"); err == nil {
		if out, err := tmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor: %v: %s", err, out)
			os.Exit(1)
		}
	}
	code := m.Run()
	tmuxCmd("kill-server").Run()
	os.Exit(code)
}

type harness struct {
	cfg            config.Config
	store          *store.Store
	tmux           *tmux.Driver
	hooks          *hooks.Manager
	gitDrv         *git.Driver
	poller         *Runner
	sessions       []store.Session
	showArchived   bool
	queuedMessages map[string]int
}
type errMsg struct{ err error }

func testPollMessage(result Result) any {
	if result.Err != nil {
		return errMsg{result.Err}
	}
	return result.Snapshot
}
func buildModel(t *testing.T) *harness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
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
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	hooks := hooks.NewManager(t.TempDir())
	gitDriver, _ := git.New()
	h := &harness{cfg: cfg, store: st, tmux: driver, hooks: hooks, gitDrv: gitDriver}
	h.poller = New(Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hooks, Git: gitDriver}, OptionsFromConfig(cfg))
	t.Cleanup(func() {
		sessions, _ := st.ListSessions(true)
		for _, s := range sessions {
			driver.Kill(s.ID)
		}
		h.poller.captureWait.Wait()
		st.Close()
	})
	return h
}
func (h *harness) refreshCmd() func() any {
	return func() any { h.poller.SetInput(h.showArchived, ""); return testPollMessage(h.poller.Step()) }
}
func (h *harness) applyCmd(t *testing.T, cmd func() any) {
	t.Helper()
	if cmd == nil {
		cmd = h.refreshCmd()
	}
	switch m := cmd().(type) {
	case errMsg:
		t.Fatal(m.err)
	case Snapshot:
		h.sessions = m.Sessions
		h.queuedMessages = m.QueuedMessages
	}
}
func (h *harness) sessionRows() []store.Session { return h.sessions }
func (h *harness) spawnSession(toolName, name, dir, group, prompt string, autoNamed, worktree bool) error {
	tool := h.cfg.Tools[toolName]
	id := uuid.NewString()[:8]
	repo, branch := "", ""
	if worktree {
		var err error
		repo, err = h.gitDrv.RepoRoot(dir)
		if err != nil {
			return err
		}
		dir, branch, err = h.gitDrv.AddWorktree(repo, name, "")
		if err != nil {
			return err
		}
	}
	plan := launch.Assemble(toolName, tool, prompt, autoNamed, false)
	command, env, err := launch.Environment(h.hooks, toolName, tool, plan.Command, id)
	if err != nil {
		return err
	}
	if err := h.tmux.Create(id, dir, command, env, 160, 30); err != nil {
		return err
	}
	now := time.Now()
	sess := store.Session{ID: id, Name: name, Tool: toolName, Cwd: dir, Group: group, Status: status.Starting, CreatedAt: now, LastStatusAt: now, TmuxSocket: h.tmux.SocketPath(), AgentSessionID: plan.AgentSessionID, WorktreeRepo: repo, WorktreeBranch: branch, PendingInputs: plan.PendingInputs, LaunchPrompt: plan.LaunchPrompt}
	if err := h.store.CreateSession(sess); err != nil {
		return err
	}
	h.sessions = append(h.sessions, sess)
	return nil
}
func createSession(t *testing.T, h *harness, name, dir, group string) {
	t.Helper()
	if err := h.spawnSession("claude", name, dir, group, "", false, false); err != nil {
		t.Fatal(err)
	}
	h.applyCmd(t, nil)
}
func createSessionOn(t *testing.T, h *harness, name, tool, dir string) {
	t.Helper()
	if err := h.spawnSession(tool, name, dir, "", "", false, false); err != nil {
		t.Fatal(err)
	}
	h.applyCmd(t, nil)
}
func createWorktreeSession(t *testing.T, h *harness, name, repo string) store.Session {
	t.Helper()
	if err := h.spawnSession("claude", name, repo, "", "", false, true); err != nil {
		t.Fatal(err)
	}
	h.applyCmd(t, nil)
	for _, s := range h.sessions {
		if s.Name == name {
			return s
		}
	}
	t.Fatal("missing session")
	return store.Session{}
}
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

// waitForAgent waits for a session's pane to hold an agent, or to be left
// with only its shell.
func waitForAgent(t *testing.T, m *harness, sessID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if running, err := sessioncmd.AgentRunning(m.tmux, sessID); err == nil && running == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane, _ := m.tmux.CapturePane(sessID)
	t.Fatalf("pane never settled on agent running = %v; pane:\n%s", want, pane)
}

// waitForPaneChild waits for a pane to run a named program, which is a
// step past waitForAgent: a shell counts as busy from the moment it forks,
// before the child has exec'd the program it was asked for.
func waitForPaneChild(t *testing.T, m *harness, sessID, name string) {
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
func quitAgent(t *testing.T, m *harness, sessID string) {
	t.Helper()
	waitForAgent(t, m, sessID, true)
	if err := m.tmux.SendKeys(sessID, "C-d"); err != nil {
		t.Fatalf("send ctrl-d: %v", err)
	}
	waitForAgent(t, m, sessID, false)
}

func assertPaneStayedOnSpawnPath(t *testing.T, m *harness, id, want string) {
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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func sessionHasPendingInput(t *testing.T, m *harness, id, want string) bool {
	t.Helper()
	for _, input := range sessionPendingInputs(t, m, id) {
		if input == want || strings.Contains(input, want) {
			return true
		}
	}
	return false
}

func sessionPendingInputs(t *testing.T, h *harness, id string) []string {
	t.Helper()
	s, err := h.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return s.PendingInputs
}

func writeName(t *testing.T, m *harness, id, name string) string {
	t.Helper()
	path := m.hooks.NameFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	request, err := hooks.NewRequestID()
	if err != nil {
		t.Fatalf("request id: %v", err)
	}
	if err := os.WriteFile(path, []byte(hooks.NameRequest(request, name)), 0o644); err != nil {
		t.Fatalf("write name file: %v", err)
	}
	return request
}

func waitForPane(t *testing.T, m *harness, id, marker string) {
	t.Helper()
	if err := m.tmux.SendText(id, marker); err != nil {
		t.Fatalf("send text: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(id)
		if err == nil && strings.Contains(pane, marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never showed %q, last capture: %q", marker, pane)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
