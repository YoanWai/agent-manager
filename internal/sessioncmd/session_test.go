package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

type sessionHarness struct {
	driver    *tmux.Driver
	store     *store.Store
	sessions  *Sessions
	terminals *Terminals
	caller    store.Session
}

// sessionConfig gives the harness one agent CLI whose command echoes the
// prompt it launched with, so a spawn's own pane proves the prompt reached
// it, plus a shell block the agent tools must refuse.
const sessionConfig = `[tools.echoer]
command = "echo"
revive_command = "echo resumed"
default_status = "idle"
activity_cutoff = "(?m)^\u276f"

[tools.flagged]
command = "echo"
prompt_flag = "-n"
default_status = "idle"
activity_cutoff = "(?m)^\u276f"

[tools.blind]
command = "echo"
default_status = "idle"

# Stands in for a CLI sitting on an approval dialog: the input line is drawn
# under it, and only the rule tells that apart from a resting prompt.
[tools.dialog]
command = "printf 'Do you want to proceed?\\n  1. Yes\\n  2. No\\nEnter to confirm\\n❯ ' && cat"
default_status = "idle"
activity_cutoff = "(?m)^❯"
rules = [{ state = "waiting", pattern = "Enter to confirm" }]

[tools.resting]
command = "printf '❯ ' && cat"
default_status = "idle"
activity_cutoff = "(?m)^❯"

[tools.picker]
command = "printf 'COMPOSER\\n' && cat"
session_store = "codex"
resume_picker_command = "printf 'COMPOSER\\n' && cat"
resume_picker_keys = "/sessions"
input_prefix = "COMPOSER"
default_status = "idle"

[tools.picker-exit]
command = "echo initial"
session_store = "codex"
resume_picker_command = "printf 'COMPOSER\\n' && cat"
resume_picker_keys = "/sessions"
input_prefix = "COMPOSER"
default_status = "idle"

[tools.terminal]
command = ""
shell = true
default_status = "idle"

[keybindings.session]
review = "ctrl+g"
`

// testConfigLoader loads the harness config the manager would, with the
// document's own tool blocks in place of the built-in CLIs, so a test gets
// a pane it can predict.
func testConfigLoader(t *testing.T, doc string) func(string) (config.Config, error) {
	t.Helper()
	var declared struct {
		Tools map[string]config.Tool `toml:"tools"`
	}
	if _, err := toml.Decode(doc, &declared); err != nil {
		t.Fatalf("decode the test tools: %v", err)
	}
	return func(dir string) (config.Config, error) {
		cfg, err := config.LoadDir(dir)
		if err != nil {
			return cfg, err
		}
		cfg.Tools = declared.Tools
		return cfg, nil
	}
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(sessionConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	driver, err := tmux.NewWithSocket("amsesstest-" + uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	callerDir := t.TempDir()
	caller := store.Session{
		ID:     uuid.NewString()[:8],
		Name:   "calling-agent",
		Tool:   "echoer",
		Cwd:    callerDir,
		Group:  "backend",
		Status: status.Idle,
	}
	if err := st.CreateGroup("backend", callerDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := driver.Create(caller.ID, caller.Cwd, "", nil, 80, 24); err != nil {
		t.Fatalf("create caller pane: %v", err)
	}
	if err := st.CreateSession(caller); err != nil {
		_ = driver.Kill(caller.ID)
		t.Fatalf("create caller row: %v", err)
	}
	newDriver := func() (*tmux.Driver, error) { return driver, nil }
	loadConfig := testConfigLoader(t, sessionConfig)
	sessions := newSessions(configDir, MCPVocabulary(), newDriver, git.New)
	sessions.loadConfig = loadConfig
	terminals := newTerminals(configDir, MCPVocabulary(), newDriver)
	terminals.loadConfig = loadConfig
	h := &sessionHarness{
		driver:    driver,
		store:     st,
		caller:    caller,
		sessions:  sessions,
		terminals: terminals,
	}
	t.Cleanup(func() { tearDownHarness(t, driver, st) })
	return h
}

func waitForSessionOutput(t *testing.T, sessions *Sessions, callerID, targetID, marker string) SessionScreen {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		screen, err := sessions.Read(callerID, targetID)
		if err == nil && strings.Contains(screen.Output, marker) {
			return screen
		}
		time.Sleep(25 * time.Millisecond)
	}
	screen, err := sessions.Read(callerID, targetID)
	t.Fatalf("session never showed %q: output=%q err=%v", marker, screen.Output, err)
	return SessionScreen{}
}

func TestSessionsListCoversAgentsOnlyAndMarksTheCaller(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	listed, err := h.sessions.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("list should hold the caller and the new agent only, got %+v", listed)
	}
	seen := map[string]Session{}
	for _, sess := range listed {
		seen[sess.ID] = sess
	}
	if !seen[h.caller.ID].Self || seen[created.ID].Self {
		t.Fatalf("self marking = %+v", listed)
	}
	if !seen[created.ID].Running || seen[created.ID].Name != "worker" {
		t.Fatalf("listed spawn = %+v", seen[created.ID])
	}
}

func TestSessionsKillKeepsTheScreenAndReviveBringsItBack(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Prompt: "hold the line"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "hold the line")

	killed, err := h.sessions.Kill(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killed.Running || killed.Status != status.Dead {
		t.Fatalf("killed session = %+v", killed)
	}
	if h.driver.Exists(created.ID) {
		t.Fatal("killed session still has a pane")
	}
	screen, err := h.sessions.Read(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Read after kill: %v", err)
	}
	if !strings.Contains(screen.Output, "hold the line") {
		t.Fatalf("a killed session should keep its last screen, got %q", screen.Output)
	}

	revived, err := h.sessions.Revive(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if !revived.Running || !h.driver.Exists(created.ID) {
		t.Fatalf("revived session = %+v", revived)
	}
	if _, err := h.sessions.Kill(h.caller.ID, h.caller.ID); err == nil {
		t.Fatal("a session must not kill itself")
	}
}

func TestSessionsArchiveHidesAndRestores(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	archived, err := h.sessions.Archive(h.caller.ID, created.ID, true)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !archived.Archived || !archived.Running {
		t.Fatalf("archiving must not stop the pane: %+v", archived)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil || !stored.Archived {
		t.Fatalf("stored archived = %+v err=%v", stored, err)
	}
	restored, err := h.sessions.Archive(h.caller.ID, created.ID, false)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Archived {
		t.Fatalf("restored session = %+v", restored)
	}
	if _, err := h.sessions.Archive(h.caller.ID, h.caller.ID, true); err == nil {
		t.Fatal("a session must not archive itself")
	}
}

// An agent that exits leaves its window open on the shell it was launched
// from. Revive puts the tool back inside that pane instead of refusing the
// row as still running, which is what the manager's own revive key does.
func TestReviveRestartsTheAgentInsideItsLivePane(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Prompt: "hold the line"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "hold the line")
	waitForAgentGone(t, h.driver, created.ID)

	revived, err := h.sessions.Revive(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if !revived.Running || revived.Status != status.Starting {
		t.Fatalf("revived session = %+v", revived)
	}
	screen := waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "resumed")
	if !strings.Contains(screen.Output, "hold the line") {
		t.Fatalf("an in-pane revive keeps what the pane already held, got %q", screen.Output)
	}
	if !h.driver.Exists(created.ID) {
		t.Fatal("revive should have left the window running")
	}
}

func TestRevivePickerRecoveryCoversBothPanePaths(t *testing.T) {
	tests := []struct {
		name string
		tool string
		kill bool
	}{
		{"create pane", "picker", true},
		{"surviving pane", "picker-exit", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSessionHarness(t)
			codexHome := t.TempDir()
			t.Setenv("CODEX_HOME", codexHome)
			if err := os.MkdirAll(filepath.Join(codexHome, "sessions"), 0o755); err != nil {
				t.Fatal(err)
			}
			created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "picker-worker", Tool: tc.tool})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if tc.kill {
				if _, err := h.sessions.Kill(h.caller.ID, created.ID); err != nil {
					t.Fatalf("Kill: %v", err)
				}
			} else {
				waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "initial")
				waitForAgentGone(t, h.driver, created.ID)
			}

			if _, err := h.sessions.Revive(h.caller.ID, created.ID); err != nil {
				t.Fatalf("Revive: %v", err)
			}
			waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "/sessions")
			stored, err := h.store.Get(created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.AgentLaunchedAt.IsZero() {
				t.Fatal("revive did not stamp its launch")
			}
			if stored.RelaunchSnapshot == nil || len(stored.RelaunchSnapshot) != 0 {
				t.Fatalf("relaunch snapshot = %v, want non-nil empty", stored.RelaunchSnapshot)
			}
		})
	}
}

func TestReviveRefusesWhileTheAgentIsStillRunning(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "chatty", Tool: "resting"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "❯")

	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err == nil ||
		!strings.Contains(err.Error(), "still running") {
		t.Fatalf("reviving a session whose agent is up = %v", err)
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

func TestSessionHarnessCleanupRemovesSocket(t *testing.T) {
	var socket string
	t.Run("harness", func(t *testing.T) {
		socket = newSessionHarness(t).driver.SocketPath()
	})
	if socket == "" {
		t.Skip("tmux not installed")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket %q survived harness cleanup: %v", socket, err)
	}
}
