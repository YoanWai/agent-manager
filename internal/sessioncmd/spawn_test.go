package sessioncmd

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/launch"
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

func TestSessionsCreateBriefsTheSpawnInTheStoredCoordinationMode(t *testing.T) {
	h := newSessionHarness(t)
	for _, mode := range []struct {
		proactive bool
		name      string
		note      string
	}{
		{proactive: false, name: "api-on-request", note: launch.OnRequestCoordinationNote},
		{proactive: true, name: "api-proactive", note: launch.ProactiveCoordinationNote},
	} {
		if err := h.store.SetProactiveCoordination(mode.proactive); err != nil {
			t.Fatal(err)
		}
		created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: mode.name, Prompt: "build the api"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		stored, err := h.store.Get(created.ID)
		if err != nil {
			t.Fatalf("stored session: %v", err)
		}
		if !strings.HasPrefix(stored.LaunchPrompt, mode.note) {
			t.Fatalf("proactive %v launched with %q, want its note first", mode.proactive, stored.LaunchPrompt)
		}
	}
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

func paneWindowSize(t *testing.T, driver *tmux.Driver, id string) (int, int) {
	t.Helper()
	panes, err := driver.Panes()
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	pane, ok := panes[id]
	if !ok {
		t.Fatalf("session %s has no pane", id)
	}
	return pane.Width, pane.Height
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

// A session's model and effort ride its launch in front of the prompt, and
// a revive carries them again on either pane path, since the row keeps the
// choice.
func TestSessionsCreateCarriesItsChoiceThroughRevive(t *testing.T) {
	for _, tc := range []struct {
		name string
		kill bool
	}{
		{"create pane", true},
		{"surviving pane", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSessionHarness(t)
			created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "loud", Prompt: "say it", Model: "big", Effort: "high"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			screen := waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "say it")
			if !strings.HasPrefix(screen.Output, "--model big --effort high ") {
				t.Fatalf("the launch dropped the choice: %q", screen.Output)
			}
			if tc.kill {
				if _, err := h.sessions.Kill(h.caller.ID, created.ID); err != nil {
					t.Fatalf("Kill: %v", err)
				}
			} else {
				waitForAgentGone(t, h.driver, created.ID)
			}
			if _, err := h.sessions.Revive(h.caller.ID, created.ID); err != nil {
				t.Fatalf("Revive: %v", err)
			}
			waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "resumed --model big --effort high")
		})
	}
}

// A spawn picks only what the CLI lists, the way the form offers only listed
// values, and a CLI that lists nothing takes no choice at all.
func TestSessionsCreateChecksTheChoiceAgainstTheCLI(t *testing.T) {
	h := newSessionHarness(t)
	for _, tc := range []struct {
		opts   CreateSessionOptions
		reason string
	}{
		{CreateSessionOptions{Model: "huge"}, `model "huge" is not one echoer lists; it lists small, big`},
		{CreateSessionOptions{Model: "big", Effort: "max"}, `effort "max" is not one big takes; it takes low, high`},
		{CreateSessionOptions{Effort: "high"}, "small takes no reasoning effort"},
		{CreateSessionOptions{Profile: "work"}, "echoer has no profiles"},
		{CreateSessionOptions{Tool: "blind", Model: "big"}, "blind does not report its models"},
	} {
		if _, err := h.sessions.Create(h.caller.ID, tc.opts); err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%+v: err = %v, want %q", tc.opts, err, tc.reason)
		}
	}
	sessions, err := h.store.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("a refused choice still spawned: %d rows", len(sessions))
	}
}

func TestSessionsCreateBranchesFromTheGroupBase(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init", "-b", "main")
	runGit("config", "user.email", "t@t")
	runGit("config", "user.name", "t")
	runGit("commit", "--allow-empty", "-m", "init")
	runGit("branch", "develop")
	runGit("checkout", "-q", "develop")
	runGit("commit", "--allow-empty", "-m", "develop work")
	runGit("checkout", "-q", "main")
	if err := h.store.SetGroupBase(h.caller.Group, "develop"); err != nil {
		t.Fatalf("set base: %v", err)
	}

	wanted := true
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{
		Name:      "develop-worker",
		Directory: repo,
		Worktree:  &wanted,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	head := exec.Command("git", "rev-parse", "HEAD")
	head.Dir = created.Directory
	out, err := head.Output()
	if err != nil {
		t.Fatalf("worktree head: %v", err)
	}
	if got, want := strings.TrimSpace(string(out)), runGit("rev-parse", "develop"); got != want {
		t.Fatalf("worktree starts at %s, want the group's base develop at %s", got, want)
	}
}

func TestSessionsCreateFetchesTheBaseUnlessTurnedOff(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	runGit := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	remote := t.TempDir()
	runGit(remote, "init", "-b", "main")
	runGit(remote, "config", "user.email", "t@t")
	runGit(remote, "config", "user.name", "t")
	runGit(remote, "commit", "--allow-empty", "-m", "init")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(remote, "clone", "-q", remote, clone)
	cached := runGit(clone, "rev-parse", "origin/main")
	runGit(remote, "commit", "--allow-empty", "-m", "after the clone")
	wanted := true
	spawnAt := func(name string) string {
		t.Helper()
		created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: name, Directory: clone, Worktree: &wanted})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		return runGit(created.Directory, "rev-parse", "HEAD")
	}

	if err := h.store.SetSetting(baseFetchSetting, "off"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if got := spawnAt("unfetched"); got != cached {
		t.Fatalf("with the fetch off the worktree starts at %s, want the cached origin/main %s", got, cached)
	}
	if err := h.store.SetSetting(baseFetchSetting, ""); err != nil {
		t.Fatalf("clear setting: %v", err)
	}
	if got, want := spawnAt("fetched"), runGit(remote, "rev-parse", "HEAD"); got != want {
		t.Fatalf("by default the worktree starts at %s, want the remote's tip %s", got, want)
	}
}

// A spawn from an agent installs the session bindings the way the manager
// does, read from the same store: the key table reaches the driver before
// the first session is created.
func TestSessionsCreateBindsTheStoredSessionKeys(t *testing.T) {
	h := newSessionHarness(t)
	reviewKey, err := keybind.Parse("ctrl+g")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	prefixKey, err := keybind.Parse("ctrl+a")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	stored := keybind.DefaultSession().
		With(keybind.Review, keybind.Keys(reviewKey)).
		With(keybind.TmuxPrefix, keybind.Keys(prefixKey))
	if err := h.store.SetKeys(stored); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "bound"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	prefix, err := exec.Command("tmux", "-L", h.driver.SocketName(), "show-options", "-v", "-t", "am_"+created.ID, "prefix").CombinedOutput()
	if err != nil || strings.TrimSpace(string(prefix)) != "C-a" {
		t.Fatalf("the session should carry the stored tmux_prefix, got %q, %v", prefix, err)
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
		t.Fatalf("ctrl+g from Settings should request the review, got:\n%s", bound)
	}
	if stale != "" {
		t.Fatalf("the default review key should not be bound alongside the configured one: %q", stale)
	}
}

// A terminal is a caller like any session now that the CLI resolves one
// from its pane, but its tool is the user's shell: a spawn from a terminal
// has no agent CLI to inherit and has to be told which one to run.
func TestSessionsCreateFromATerminalTakesTheSettingsTool(t *testing.T) {
	h := newSessionHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create terminal: %v", err)
	}
	if err := h.store.SetDefaultTool("flagged"); err != nil {
		t.Fatalf("set default tool: %v", err)
	}
	created, err := h.sessions.Create(terminal.ID, CreateSessionOptions{Prompt: "ship the fix"})
	if err != nil {
		t.Fatalf("Create from a terminal: %v", err)
	}
	if created.Tool != "flagged" || created.Group != terminal.Group || !created.Running {
		t.Fatalf("created from a terminal = %+v, terminal = %+v", created, terminal)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "ship the fix")
}

func TestSessionsCreateWithNoCallerTakesTheSettingsDefaults(t *testing.T) {
	h := newSessionHarness(t)
	scriptDir := t.TempDir()
	t.Chdir(scriptDir)
	if err := h.store.SetDefaultTool("flagged"); err != nil {
		t.Fatalf("set default tool: %v", err)
	}
	created, err := h.sessions.Create("", CreateSessionOptions{Name: "ticket-123", Prompt: "fix ticket 123"})
	if err != nil {
		t.Fatalf("Create with no caller: %v", err)
	}
	wantDir, _ := filepath.EvalSymlinks(scriptDir)
	gotDir, _ := filepath.EvalSymlinks(created.Directory)
	if created.Tool != "flagged" || created.Group != "" || gotDir != wantDir || !created.Running {
		t.Fatalf("created with no caller = %+v, want flagged in the root group at %s", created, wantDir)
	}
	row, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.ParentID != "" {
		t.Fatalf("a session with no caller hangs under %q", row.ParentID)
	}
	waitForSessionOutput(t, h.sessions, "", created.ID, "fix ticket 123")

	if err := h.store.SetHiddenTools(map[string]bool{"flagged": true}); err != nil {
		t.Fatalf("hide the default tool: %v", err)
	}
	fallback, err := h.sessions.Create("", CreateSessionOptions{Name: "ticket-124"})
	if err != nil {
		t.Fatalf("Create with the default tool hidden: %v", err)
	}
	if fallback.Tool != "blind" {
		t.Fatalf("tool with the default hidden = %q, want the first enabled one, blind", fallback.Tool)
	}

	backend := "backend"
	filed, err := h.sessions.Create("", CreateSessionOptions{Tool: "echoer", Group: &backend})
	if err != nil {
		t.Fatalf("Create into a group with no caller: %v", err)
	}
	groupDir, _ := filepath.EvalSymlinks(h.caller.Cwd)
	if gotDir, _ := filepath.EvalSymlinks(filed.Directory); filed.Group != "backend" || gotDir != groupDir {
		t.Fatalf("created into backend = %+v, want the group's directory %s", filed, groupDir)
	}

	runtime, err := h.sessions.open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	everyTool := map[string]bool{}
	for _, name := range runtime.cfg.AgentToolNames() {
		everyTool[name] = true
	}
	runtime.store.Close()
	if err := h.store.SetHiddenTools(everyTool); err != nil {
		t.Fatalf("hide every tool: %v", err)
	}
	_, err = h.sessions.Create("", CreateSessionOptions{})
	if err == nil || !strings.Contains(err.Error(), "create_session tool") {
		t.Fatalf("Create with every tool hidden = %v, want a request to name one", err)
	}
	shell, _ := runtime.cfg.ShellTool()
	_, listed, _ := strings.Cut(err.Error(), "(configured tools are ")
	if slices.Contains(strings.Split(strings.TrimSuffix(listed, ")"), ", "), shell) {
		t.Fatalf("the error offers the shell tool %q as a choice: %v", shell, err)
	}
}
