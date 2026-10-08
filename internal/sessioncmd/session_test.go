package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/YoanWai/agent-manager/internal/catalog"
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
catalog = "stand-in"
model_args = "--model {model}"
effort_args = "--effort {effort}"
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

# Stands in for a dialog that replaces the composer's input line.
[tools.dialog-hidden-composer]
command = "printf 'Do you want to proceed?\\n  1. Yes\\n  2. No\\nEnter to confirm\\n' && cat"
default_status = "idle"
activity_cutoff = "(?m)^❯"
rules = [{ state = "waiting", pattern = "Enter to confirm" }]

[tools.resting]
command = "printf '❯ ' && cat"
default_status = "idle"
activity_cutoff = "(?m)^❯"

[tools.picker]
command = "printf 'COMPOSER ' && cat"
session_store = "codex"
resume_picker_command = "printf 'COMPOSER ' && cat"
resume_picker_keys = "/sessions"
input_prefix = "COMPOSER"
default_status = "idle"

[tools.picker-exit]
command = "echo initial"
session_store = "codex"
resume_picker_command = "printf 'COMPOSER ' && cat"
resume_picker_keys = "/sessions"
input_prefix = "COMPOSER"
default_status = "idle"

[tools.terminal]
command = ""
shell = true
default_status = "idle"
`

// testConfigLoader stands the document's tool blocks in for the built-in
// CLIs, so a test gets a pane it can predict.
func testConfigLoader(t *testing.T, doc string) func() (config.Config, error) {
	t.Helper()
	var declared struct {
		Tools map[string]config.Tool `toml:"tools"`
	}
	if _, err := toml.Decode(doc, &declared); err != nil {
		t.Fatalf("decode the test tools: %v", err)
	}
	return func() (config.Config, error) {
		return config.Config{Tools: declared.Tools}, nil
	}
}

func newStoreSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
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
	if err := st.CreateSession(caller); err != nil {
		t.Fatalf("create caller row: %v", err)
	}
	newDriver := func() (*tmux.Driver, error) { return driver, nil }
	loadConfig := testConfigLoader(t, sessionConfig)
	sessions := newSessions(configDir, MCPVocabulary(), newDriver, git.New)
	sessions.loadConfig = loadConfig
	h := &sessionHarness{
		driver:   driver,
		store:    st,
		caller:   caller,
		sessions: sessions,
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close harness store: %v", err)
		}
	})
	return h
}

// addSessionRow models a valid command caller without starting a pane.
func (h *sessionHarness) addSessionRow(t *testing.T, name string) string {
	t.Helper()
	id := uuid.NewString()[:8]
	if err := h.store.CreateSession(store.Session{
		ID: id, Name: name, Tool: h.caller.Tool, Cwd: h.caller.Cwd, Status: status.Idle,
	}); err != nil {
		t.Fatalf("create session row: %v", err)
	}
	return id
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
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
	sessions.loadCatalog = func(string, string, config.Tool) (catalog.Catalog, error) {
		return catalog.Catalog{Models: []catalog.Model{
			{ID: "small", Default: true},
			{ID: "big", Efforts: []string{"low", "high"}},
		}}, nil
	}
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
func TestSessionsWithNoCallerListAndReadButStillRefuseToMessage(t *testing.T) {
	h := newSessionHarness(t)
	listed, err := h.sessions.List("")
	if err != nil {
		t.Fatalf("List with no caller: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != h.caller.ID || listed[0].Self {
		t.Fatalf("listed with no caller = %+v", listed)
	}
	groups, err := h.sessions.Groups("")
	if err != nil || len(groups) != 1 || groups[0].Path != "backend" {
		t.Fatalf("Groups with no caller = %+v, %v", groups, err)
	}
	if _, err := h.sessions.Read("", h.caller.ID); err != nil {
		t.Fatalf("Read with no caller: %v", err)
	}
	if _, err := h.sessions.Send("", h.caller.ID, "hello"); err == nil || !strings.Contains(err.Error(), "not inside an Agent Manager session") {
		t.Fatalf("Send with no caller = %v, want the missing caller named", err)
	}
	if _, err := h.sessions.Kill("", h.caller.ID); err == nil || !strings.Contains(err.Error(), "not inside an Agent Manager session") {
		t.Fatalf("Kill with no caller = %v, want the missing caller named", err)
	}
}
