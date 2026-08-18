package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	if created.ID == "" || created.Name != "terminal-"+h.caller.Name {
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

	nest := false
	group := "platform/api"
	inherited, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group, Nest: &nest})
	if err != nil {
		t.Fatalf("create inherited: %v", err)
	}
	if inherited.Group != group || !sameTerminalPath(inherited.Directory, parentDir) {
		t.Fatalf("inherited target = %+v", inherited)
	}

	root := ""
	explicit, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &root, Directory: explicitDir, Nest: &nest})
	if err != nil {
		t.Fatalf("create explicit: %v", err)
	}
	if explicit.Group != "" || !sameTerminalPath(explicit.Directory, explicitDir) {
		t.Fatalf("explicit target = %+v", explicit)
	}
	if err := h.store.SetGroupArchived("platform", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group, Nest: &nest}); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("archived group error = %v", err)
	}

	missing := "missing/group"
	before, _ := h.store.ListSessions(false)
	if _, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &missing, Nest: &nest}); err == nil || !strings.Contains(err.Error(), "does not exist") {
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

func TestCreateNestsUnderCallerByDefault(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ParentID != h.caller.ID || created.ParentName != h.caller.Name {
		t.Fatalf("created = %+v", created)
	}
}

// A terminal an agent opens for itself is named after that agent, and the
// next one counts up: the name is what tells one row from the next in the
// list the user reads.
func TestCreateNamesTerminalsAfterTheirSession(t *testing.T) {
	h := newTerminalHarness(t)
	first, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if want := "terminal-" + h.caller.Name; first.Name != want {
		t.Fatalf("first name = %q, want %q", first.Name, want)
	}
	if want := "terminal-" + h.caller.Name + "-2"; second.Name != want {
		t.Fatalf("second name = %q, want %q", second.Name, want)
	}
}

// An un-nested terminal has no session to name it after, so it keeps the
// generated name.
func TestCreateNestFalseKeepsTheGeneratedName(t *testing.T) {
	h := newTerminalHarness(t)
	nest := false
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Nest: &nest})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !regexp.MustCompile(`^terminal-[0-9a-f]{4}$`).MatchString(created.Name) {
		t.Fatalf("name = %q, want the generated terminal-<4 hex>", created.Name)
	}
}

func TestListAndReadCarryParentMetadata(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	listed, err := h.terminals.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, term := range listed {
		if term.ID != created.ID {
			continue
		}
		found = true
		if term.ParentID != h.caller.ID || term.ParentName != h.caller.Name {
			t.Fatalf("listed = %+v", term)
		}
	}
	if !found {
		t.Fatalf("terminal %s missing from %+v", created.ID, listed)
	}
	screen, err := h.terminals.Read(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if screen.Terminal.ParentID != h.caller.ID || screen.Terminal.ParentName != h.caller.Name {
		t.Fatalf("screen = %+v", screen.Terminal)
	}
}

func TestListAndReadKeepAnOrphanedTerminal(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.Delete(h.caller.ID); err != nil {
		t.Fatalf("delete caller: %v", err)
	}
	other := store.Session{
		ID:     uuid.NewString()[:8],
		Name:   "reader",
		Tool:   "claude",
		Cwd:    h.caller.Cwd,
		Group:  h.caller.Group,
		Status: status.Idle,
	}
	if err := h.store.CreateSession(other); err != nil {
		t.Fatalf("reader: %v", err)
	}
	listed, err := h.terminals.List(other.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, term := range listed {
		if term.ID != created.ID {
			continue
		}
		found = true
		if term.ParentID != h.caller.ID || term.ParentName != "" {
			t.Fatalf("orphan listed = %+v", term)
		}
	}
	if !found {
		t.Fatalf("orphan missing from %+v", listed)
	}
	screen, err := h.terminals.Read(other.ID, created.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if screen.Terminal.ParentID != h.caller.ID || screen.Terminal.ParentName != "" {
		t.Fatalf("orphan screen = %+v", screen.Terminal)
	}
}

func TestCreateFromAShellJoinsItsSiblings(t *testing.T) {
	h := newTerminalHarness(t)
	first, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, err := h.terminals.Create(first.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create from shell: %v", err)
	}
	if second.ParentID != h.caller.ID || second.Group != first.Group || !sameTerminalPath(second.Directory, first.Directory) {
		t.Fatalf("second = %+v, first = %+v", second, first)
	}
	// Siblings share a parent, so they share the name it gives them.
	if want := "terminal-" + h.caller.Name + "-2"; second.Name != want {
		t.Fatalf("sibling name = %q, want %q", second.Name, want)
	}
	nest := false
	loose, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Nest: &nest})
	if err != nil {
		t.Fatalf("Create un-nested: %v", err)
	}
	fromLoose, err := h.terminals.Create(loose.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create from un-nested shell: %v", err)
	}
	if fromLoose.ParentID != "" || fromLoose.Group != loose.Group || !sameTerminalPath(fromLoose.Directory, loose.Directory) {
		t.Fatalf("from un-nested shell = %+v, loose = %+v", fromLoose, loose)
	}
	if !regexp.MustCompile(`^terminal-[0-9a-f]{4}$`).MatchString(fromLoose.Name) {
		t.Fatalf("from un-nested shell name = %q, want the generated terminal-<4 hex>", fromLoose.Name)
	}
}

func TestCreateNestFalseIsUnnested(t *testing.T) {
	h := newTerminalHarness(t)
	nest := false
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Nest: &nest})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ParentID != "" {
		t.Fatalf("parent = %q", created.ParentID)
	}
}

func TestCreateNestTrueRejectsOtherGroup(t *testing.T) {
	h := newTerminalHarness(t)
	if err := h.store.CreateGroup("elsewhere", h.caller.Cwd); err != nil {
		t.Fatalf("group: %v", err)
	}
	group := "elsewhere"
	_, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group})
	if err == nil || !strings.Contains(err.Error(), "nest false") {
		t.Fatalf("omitted nest err = %v", err)
	}
	nest := true
	_, err = h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group, Nest: &nest})
	if err == nil || !strings.Contains(err.Error(), "nest false") {
		t.Fatalf("explicit nest err = %v", err)
	}
}

func TestCloseDeletesShellAndRefusesAgent(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.terminals.Close(h.caller.ID, h.caller.ID); err == nil {
		t.Fatal("close agent")
	}
	if err := h.terminals.Close(h.caller.ID, created.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := h.store.Get(created.ID); err == nil {
		t.Fatal("row still present")
	}
	if h.driver.Exists(created.ID) {
		t.Fatal("pane still live")
	}
}

func TestCloseRefusesTerminalOfAnotherSession(t *testing.T) {
	h := newTerminalHarness(t)
	other := store.Session{
		ID:     uuid.NewString()[:8],
		Name:   "other-agent",
		Tool:   "claude",
		Cwd:    h.caller.Cwd,
		Group:  h.caller.Group,
		Status: status.Idle,
	}
	if err := h.store.CreateSession(other); err != nil {
		t.Fatalf("other agent: %v", err)
	}
	created, err := h.terminals.Create(other.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.terminals.Close(h.caller.ID, created.ID); err == nil {
		t.Fatal("closed another session's terminal")
	}
	if _, err := h.store.Get(created.ID); err != nil {
		t.Fatalf("row gone: %v", err)
	}
	if !h.driver.Exists(created.ID) {
		t.Fatal("pane killed")
	}
}

func TestCloseRefusesATerminalMovedOut(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.PlaceSession(created.ID, h.caller.Group, ""); err != nil {
		t.Fatalf("unnest: %v", err)
	}
	if err := h.terminals.Close(h.caller.ID, created.ID); err == nil {
		t.Fatal("closed a terminal that moved out")
	}
	if _, err := h.store.Get(created.ID); err != nil {
		t.Fatalf("row gone: %v", err)
	}
	if !h.driver.Exists(created.ID) {
		t.Fatal("pane killed")
	}
}

func TestCloseRefusesUnnestedTerminal(t *testing.T) {
	h := newTerminalHarness(t)
	nest := false
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Nest: &nest})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.terminals.Close(h.caller.ID, created.ID); err == nil {
		t.Fatal("closed an un-nested terminal")
	}
	if _, err := h.store.Get(created.ID); err != nil {
		t.Fatalf("row gone: %v", err)
	}
	if !h.driver.Exists(created.ID) {
		t.Fatal("pane killed")
	}
}
