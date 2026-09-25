package sessioncmd

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestSessionsCreateCarriesNamePromptAndTargetWithRealTmux(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{
		Name:   "payments-retry-fix",
		Prompt: "fix the retry backoff",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "payments-retry-fix" || created.Tool != "echoer" || !created.Running {
		t.Fatalf("created identity = %+v", created)
	}
	if created.Group != h.caller.Group || !sameTerminalPath(created.Directory, h.caller.Cwd) {
		t.Fatalf("created target = %+v, caller = %+v", created, h.caller)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("stored session: %v", err)
	}
	if stored.Name != created.Name || stored.Tool != "echoer" || stored.Status != status.Starting {
		t.Fatalf("stored row = %+v", stored)
	}
	// echo prints what the launch command handed it, so the pane proves the
	// prompt rode the command line rather than being dropped.
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "fix the retry backoff")
}

func TestSessionsCreateAutoNamesAndAsksForARename(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "build the api"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "echoer-"+created.ID[:4] {
		t.Fatalf("auto-named session = %q", created.Name)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "build the api")
}

func TestSessionsCreateRejectsShellsUnknownToolsAndFlagPrompts(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "terminal"}); err == nil ||
		!strings.Contains(err.Error(), "create_terminal") {
		t.Fatalf("shell tool error = %v", err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "nope"}); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unknown tool error = %v", err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "--help"}); err == nil ||
		!strings.Contains(err.Error(), "read it as a flag") {
		t.Fatalf("flag-like prompt error = %v", err)
	}
	// A tool that takes its prompt behind a flag can carry one safely, and a
	// bullet list is an ordinary way to write a task.
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "flagged", Prompt: "- do the thing"}); err != nil {
		t.Fatalf("a flagged tool should accept a prompt starting with a dash: %v", err)
	}
	missing := "missing-group"
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Group: &missing}); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown group error = %v", err)
	}
}

func TestSessionsCreateOpensItsOwnWorktreeWhenAsked(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	wanted := true
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{
		Name:      "worktree-worker",
		Directory: repo,
		Worktree:  &wanted,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Branch == "" {
		t.Fatalf("worktree session = %+v, want a branch", created)
	}
	if sameTerminalPath(created.Directory, repo) {
		t.Fatalf("worktree session should work outside the main checkout, got %q", created.Directory)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("stored session: %v", err)
	}
	if stored.WorktreeRepo == "" || stored.WorktreeBranch != created.Branch {
		t.Fatalf("stored worktree = %+v", stored)
	}
}

// A spawn from an agent installs the session bindings the way the manager
// does, read from the same config: the key table reaches the driver before
// the first session is created.
func TestSessionsCreateBindsTheConfiguredSessionKeys(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "bound"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	bound, err := exec.Command("tmux", "-L", h.driver.SocketName(), "list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list-keys: %v: %s", err, bound)
	}
	review, stale := "", ""
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(line, tmux.RequestReview) {
			continue
		}
		switch fields[3] {
		case "C-g":
			review = line
		case "C-r":
			stale = line
		}
	}
	if review == "" {
		t.Fatalf("ctrl+g from config.toml should request the review, got:\n%s", bound)
	}
	if stale != "" {
		t.Fatalf("the default review key should not be bound alongside the configured one: %q", stale)
	}
}

// Nothing outside the manager can measure the preview panel, and tmux
// hands an unsized detached session 80x24, so every pane these tools open
// comes up narrower than the panel that has to draw it. They take the box
// the running manager recorded instead.
func TestHeadlessLaunchesUseTheManagersPaneSize(t *testing.T) {
	h := newSessionHarness(t)
	if err := h.store.SetPaneSize(131, 47); err != nil {
		t.Fatalf("set pane size: %v", err)
	}

	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if width, height := paneWindowSize(t, h.driver, created.ID); width != 131 || height != 47 {
		t.Fatalf("created pane = %dx%d, want 131x47", width, height)
	}

	if _, err := h.sessions.Kill(h.caller.ID, created.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if width, height := paneWindowSize(t, h.driver, created.ID); width != 131 || height != 47 {
		t.Fatalf("revived pane = %dx%d, want 131x47", width, height)
	}
}

// A terminal is a caller like any session now that the CLI resolves one
// from its pane, but its tool is the user's shell: a spawn from a terminal
// has no agent CLI to inherit and has to be told which one to run.
func TestSessionsCreateFromATerminalAsksForATool(t *testing.T) {
	h := newSessionHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create terminal: %v", err)
	}
	_, err = h.sessions.Create(terminal.ID, CreateSessionOptions{Prompt: "ship the fix"})
	if err == nil {
		t.Fatal("a toolless spawn from a terminal succeeded")
	}
	for _, want := range []string{"create_session tool", "echoer"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	runtime, openErr := h.sessions.open()
	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}
	shell, _ := runtime.cfg.ShellTool()
	runtime.store.Close()
	_, listed, _ := strings.Cut(err.Error(), "(configured tools are ")
	offered := strings.Split(strings.TrimSuffix(listed, ")"), ", ")
	if slices.Contains(offered, shell) {
		t.Fatalf("the error offers the shell tool %q as a choice: %v", shell, err)
	}

	created, err := h.sessions.Create(terminal.ID, CreateSessionOptions{Tool: "echoer", Prompt: "ship the fix"})
	if err != nil {
		t.Fatalf("Create with a tool named: %v", err)
	}
	if created.Tool != "echoer" || created.Group != terminal.Group || !created.Running {
		t.Fatalf("created from a terminal = %+v, terminal = %+v", created, terminal)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "ship the fix")
}
