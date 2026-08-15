package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

type terminalHarness struct {
	driver    *tmux.Driver
	store     *store.Store
	terminals *Terminals
	caller    store.Session
}

func newTerminalHarness(t *testing.T) *terminalHarness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
	configText := `[tools.terminal]
command = ""
shell = true
default_status = "idle"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configText), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	driver, err := tmux.NewWithSocket("amtermtest-" + uuid.NewString()[:8])
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
		Tool:   "claude",
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
	h := &terminalHarness{
		driver: driver,
		store:  st,
		caller: caller,
	}
	h.terminals = newTerminals(configDir, MCPVocabulary(), func() (*tmux.Driver, error) { return driver, nil })
	t.Cleanup(func() {
		sessions, _ := st.ListSessions(true)
		for _, sess := range sessions {
			_ = driver.Kill(sess.ID)
		}
		if out, err := exec.Command("tmux", "-L", driver.SocketName(), "kill-server").CombinedOutput(); err != nil && !strings.Contains(string(out), "no server running") {
			t.Errorf("kill test tmux server: %v: %s", err, strings.TrimSpace(string(out)))
		}
		_ = st.Close()
	})
	return h
}

func waitForTerminalOutput(t *testing.T, terminals *Terminals, callerID, terminalID, marker string) TerminalScreen {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		screen, err := terminals.Read(callerID, terminalID)
		if err == nil && strings.Contains(screen.Output, marker) {
			return screen
		}
		time.Sleep(25 * time.Millisecond)
	}
	screen, err := terminals.Read(callerID, terminalID)
	t.Fatalf("terminal never showed %q: output=%q err=%v", marker, screen.Output, err)
	return TerminalScreen{}
}

func sameTerminalPath(left, right string) bool {
	resolvedLeft, leftErr := filepath.EvalSymlinks(left)
	resolvedRight, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && resolvedLeft == resolvedRight
}

func TestTerminalsCreateListSendAndReadWithRealTmux(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" || created.Name != "terminal-"+created.ID[:4] {
		t.Fatalf("created identity = %+v", created)
	}
	if created.Group != h.caller.Group || !sameTerminalPath(created.Directory, h.caller.Cwd) || !created.Running {
		t.Fatalf("created target = %+v, caller = %+v", created, h.caller)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("stored terminal: %v", err)
	}
	if stored.Tool != "terminal" || stored.Status != status.Starting {
		t.Fatalf("stored terminal = %+v", stored)
	}

	listed, err := h.terminals.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || !listed[0].Running {
		t.Fatalf("listed = %+v", listed)
	}

	const commandMarker = "terminal-command-marker"
	sent, err := h.terminals.Send(h.caller.ID, created.ID, "printf '"+commandMarker+"\\n'", nil)
	if err != nil {
		t.Fatalf("Send command: %v", err)
	}
	if sent.TerminalID != created.ID || sent.Sent != "command" {
		t.Fatalf("send result = %+v", sent)
	}
	screen := waitForTerminalOutput(t, h.terminals, h.caller.ID, created.ID, commandMarker)
	if strings.Contains(screen.Output, "\x1b[") {
		t.Fatalf("read output still carries ANSI escapes: %q", screen.Output)
	}

	const keyMarker = "terminal-keys-marker"
	keyed, err := h.terminals.Send(h.caller.ID, created.ID, "", []string{"printf '" + keyMarker + "\\n'", "Enter"})
	if err != nil {
		t.Fatalf("Send keys: %v", err)
	}
	if keyed.Sent != "keys" || keyed.TerminalID != created.ID {
		t.Fatalf("send keys result = %+v", keyed)
	}
	waitForTerminalOutput(t, h.terminals, h.caller.ID, created.ID, keyMarker)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home directory: %v", err)
	}
	homeTerminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Directory: "~"})
	if err != nil {
		t.Fatalf("Create in home: %v", err)
	}
	if !sameTerminalPath(homeTerminal.Directory, home) {
		t.Fatalf("home terminal directory = %q, want %q", homeTerminal.Directory, home)
	}
}

func TestCreateTerminalResolvesExplicitGroupAndDirectory(t *testing.T) {
	h := newTerminalHarness(t)
	parentDir := t.TempDir()
	explicitDir := t.TempDir()
	if err := h.store.CreateGroup("platform", parentDir); err != nil {
		t.Fatal(err)
	}
	if err := h.store.CreateGroup("platform/api", ""); err != nil {
		t.Fatal(err)
	}

	group := "platform/api"
	inherited, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group})
	if err != nil {
		t.Fatalf("create inherited: %v", err)
	}
	if inherited.Group != group || !sameTerminalPath(inherited.Directory, parentDir) {
		t.Fatalf("inherited target = %+v", inherited)
	}

	root := ""
	explicit, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &root, Directory: explicitDir})
	if err != nil {
		t.Fatalf("create explicit: %v", err)
	}
	if explicit.Group != "" || !sameTerminalPath(explicit.Directory, explicitDir) {
		t.Fatalf("explicit target = %+v", explicit)
	}
	if err := h.store.SetGroupArchived("platform", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group}); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("archived group error = %v", err)
	}

	missing := "missing/group"
	before, _ := h.store.ListSessions(false)
	if _, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &missing}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing group error = %v", err)
	}
	after, _ := h.store.ListSessions(false)
	if len(after) != len(before) {
		t.Fatalf("failed create added a row: before=%d after=%d", len(before), len(after))
	}
}

func TestTerminalCommandsRejectUnsafeTargetsAndInvalidInput(t *testing.T) {
	h := newTerminalHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		command string
		keys    []string
	}{
		{"neither", "", nil},
		{"both", "pwd", []string{"Enter"}},
		{"empty key", "", []string{""}},
	} {
		if _, err := h.terminals.Send(h.caller.ID, terminal.ID, test.command, test.keys); err == nil {
			t.Fatalf("%s input was accepted", test.name)
		}
	}
	if _, err := h.terminals.Send(h.caller.ID, h.caller.ID, "pwd", nil); err == nil || !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("agent target error = %v", err)
	}
	if err := h.driver.Kill(terminal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.terminals.Send(h.caller.ID, terminal.ID, "pwd", nil); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("dead send error = %v", err)
	}
	if _, err := h.terminals.Read(h.caller.ID, terminal.ID); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("dead read error = %v", err)
	}
	if _, err := h.terminals.List(""); err == nil || !strings.Contains(err.Error(), "AGENT_MANAGER_SESSION_ID") {
		t.Fatalf("missing caller error = %v", err)
	}
}
