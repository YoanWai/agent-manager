package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

func TestExpandForkCommandQuotesPlaceholders(t *testing.T) {
	got := uiapply.ExpandCommand("tool --fork {id} --new {new_id} --name {name} --file {session_file}", "source", "new", "Sam's fork", "/store/session.jsonl")
	want := "tool --fork 'source' --new 'new' --name 'Sam'\\''s fork' --file '/store/session.jsonl'"
	if runtime.GOOS == "windows" {
		// The pane shell is PowerShell, which doubles a quote inside a
		// single-quoted string.
		want = "tool --fork 'source' --new 'new' --name 'Sam''s fork' --file '/store/session.jsonl'"
	}
	if got != want {
		t.Fatalf("fork command = %q, want %q", got, want)
	}
}

// captureArgsCommand is a fork command that writes the expanded
// placeholders to file, one per line, and then runs cat. Whatever the launch
// appends after the command (an --mcp-config, say) lands on the trailing
// cat, not in the file.
func captureArgsCommand(file string, placeholders ...string) string {
	if runtime.GOOS == "windows" {
		return testfixture.ModeCommand("capture-args "+tmux.ShellQuote(file)) + " " +
			strings.Join(placeholders, " ") + "; " + testfixture.Cat()
	}
	return "printf '%s\\n' " + strings.Join(placeholders, " ") + " > " + tmux.ShellQuote(file) + "; cat"
}

func TestForkSelectedSessionCreatesNamedSibling(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "source", dir, "work")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = captureArgsCommand(argsFile, "{id}", "{new_id}", "{name}")
	m.services.cfg.Tools[source.Tool] = tool

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(*Model)
	if !forkOpen(m) || openFork(m).Root().(forkRoot).source.ID != source.ID {
		t.Fatalf("fork screen = %v, source = %q", m.screenID(), openFork(m).Root().(forkRoot).source.ID)
	}
	openFork(m).SetName("child fork")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !m.onList() || m.errBar.text != "" {
		t.Fatalf("after fork: screen = %v err=%q", m.screenID(), m.errBar.text)
	}

	var forkedID string
	for _, sess := range m.sessionRows() {
		if sess.Name != "child fork" {
			continue
		}
		forkedID = sess.ID
		if sess.Tool != source.Tool || sess.Cwd != source.Cwd || sess.Group != source.Group {
			t.Fatalf("forked session = %+v, source = %+v", sess, source)
		}
		if sess.AgentSessionID == "" || sess.AgentSessionID == source.AgentSessionID {
			t.Fatalf("forked conversation id = %q", sess.AgentSessionID)
		}
	}
	if forkedID == "" {
		t.Fatal("forked session not found")
	}
	stored, err := m.services.store.Get(forkedID)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		raw, err = os.ReadFile(argsFile)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"source-conversation", stored.AgentSessionID, "child fork"}
	if len(got) != len(want) {
		t.Fatalf("fork args = %q", raw)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fork args = %v, want %v", got, want)
		}
	}
}

// The source switched conversation after the row was listed; the fork
// branches from the one it is on now.
func TestForkBranchesFromTheConversationTheSourceReported(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	source := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(source.ID, "launch-conversation"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	if result, err := m.services.store.ReportAgentSessionID(source.ID, source.Tool, "switched", 0); err != nil || result != store.ReportAdopted {
		t.Fatalf("report: %v %v", result, err)
	}
	m.selectSessionRow(t, "source")
	if row, _ := m.selectedRow(); row.sess.AgentSessionID != "launch-conversation" {
		listed := row.sess.AgentSessionID
		t.Fatalf("listed row is on %q; the test needs it a poll behind", listed)
	}
	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = captureArgsCommand(argsFile, "{id}")
	m.services.cfg.Tools[source.Tool] = tool

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(*Model)
	if !forkOpen(m) {
		t.Fatalf("fork screen = %v", m.screenID())
	}
	openFork(m).SetName("child")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("fork: %q", m.errBar.text)
	}
	if args := readWhenWritten(t, argsFile); args != "switched\n" {
		t.Fatalf("fork args = %q, want the reported conversation", args)
	}
}

// A fork launches on its source's model and keeps it for its own relaunches.
func TestForkCarriesTheSourceChoice(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	tool := m.services.cfg.Tools["claude"]
	tool.ModelArgs = "--model {model}"
	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool.ForkCommand = argCaptureCommand(argsFile) + " {id}"
	m.services.cfg.Tools["claude"] = tool
	choice := config.Choice{Model: "opus"}
	if err := m.spawnSession("claude", "source", t.TempDir(), "", "", false, false, choice); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	source := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")
	m.openFork()
	openFork(m).SetName("child")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("fork: %q", m.errBar.text)
	}
	if args := readWhenWritten(t, argsFile); !strings.HasPrefix(args, "source-conversation\n--model\nopus\n") {
		t.Fatalf("fork launch arguments = %q", args)
	}
	for _, sess := range m.sessionRows() {
		if sess.Name != "child" {
			continue
		}
		stored, err := m.services.store.Get(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Choice != choice {
			t.Fatalf("fork choice = %+v, want %+v", stored.Choice, choice)
		}
		return
	}
	t.Fatal("forked session not found")
}

func TestForkCopiesManagedWorktreeReference(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "managed-source",
		Name:           "source",
		Tool:           "claude",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "source-conversation",
		WorktreeRepo:   filepath.Dir(dir),
		WorktreeBranch: "am/source",
	}
	if err := m.services.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")
	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {id}; cat"
	m.services.cfg.Tools[source.Tool] = tool

	m.openFork()
	openFork(m).SetName("forked")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "forked" {
			forked = sess
			break
		}
	}
	if forked.ID == "" {
		t.Fatal("forked session not found")
	}
	if forked.WorktreeRepo != source.WorktreeRepo || forked.WorktreeBranch != source.WorktreeBranch {
		t.Fatalf("forked worktree reference = %+v, source = %+v", forked, source)
	}
}

func TestForkLaunchFailureKeepsSharedWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := seedRepo(t)
	if err := m.spawnSession("claude", "source", repo, "", "", false, true, config.Choice{}); err != nil {
		t.Fatal(err)
	}
	sessions, err := m.services.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, err %v", sessions, err)
	}
	source := sessions[0]
	badConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(badConfig, "hooks"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.services.hooks = hooks.NewManager(badConfig)
	m.services.lifecycle, err = sessioncmd.NewLifecycle(sessioncmd.Runtime{
		Config: m.services.cfg, Store: m.services.store, Driver: m.services.tmux,
		Hooks: m.services.hooks, Git: m.services.gitDrv, Snapshot: m.services.setSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}

	forked := source
	forked.ID = "failed-fork"
	forked.Name = "forked"
	if err := m.launchNewSession(forked, m.services.cfg.Tools[forked.Tool], "cat", launchOptions{}); err == nil {
		t.Fatal("launch failure was not reported")
	}
	if _, err := os.Stat(source.Cwd); err != nil {
		t.Fatalf("source worktree was removed: %v", err)
	}
}

func TestOpenForkRequiresConfiguredCommandAndConversationID(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)

	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = ""
	m.services.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "no fork_command") {
		t.Fatalf("missing command error = %q", m.errBar.text)
	}

	tool.ForkCommand = "tool --fork latest"
	m.services.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "must reference the source via {id} or {session_file}") {
		t.Fatalf("missing placeholder error = %q", m.errBar.text)
	}

	tool.ForkCommand = "tool --fork {id}"
	m.services.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "no captured conversation id") {
		t.Fatalf("missing id error = %q", m.errBar.text)
	}
}

// Every fork_command the manager ships has to satisfy its own validator, so
// a placeholder can never outrun the session store that resolves it.
func TestShippedForkCommandsValidate(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools) < 6 {
		t.Fatalf("built-in tools = %d, expected every shipped CLI", len(cfg.Tools))
	}
	source := store.Session{Name: "source", AgentSessionID: "source-conversation"}
	forkable := 0
	for name, tool := range cfg.Tools {
		if tool.Shell || tool.ForkCommand == "" {
			continue
		}
		forkable++
		if err := uiapply.ValidateForkSource(name, tool, source); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if forkable == 0 {
		t.Fatal("no shipped tool defines a fork_command")
	}
}

// {session_file} only resolves for a store that keeps conversations in a
// file. A tool without one would otherwise fork against a gemini path.
func TestOpenForkRejectsSessionFileWithoutAFileBackedStore(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "claude-source",
		Name:           "source",
		Tool:           "claude",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "claude-conversation",
	}
	if err := m.services.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")

	previousResolver := uiapply.ForkSessionFile
	uiapply.ForkSessionFile = func(sessionStore, id string) (string, error) {
		t.Errorf("resolver called for session store %q, id %q", sessionStore, id)
		return "", nil
	}
	t.Cleanup(func() { uiapply.ForkSessionFile = previousResolver })

	tool := m.services.cfg.Tools["claude"]
	if tool.SessionStore != "" {
		t.Fatalf("claude session_store = %q, want empty", tool.SessionStore)
	}
	tool.ForkCommand = "claude --session-file {session_file}"
	m.services.cfg.Tools["claude"] = tool

	m.openFork()

	if forkOpen(m) {
		t.Fatal("fork opened for a tool whose store keeps no session file")
	}
	if !strings.Contains(m.errBar.text, "{session_file}") ||
		!strings.Contains(m.errBar.text, `session_store = "gemini"`) {
		t.Fatalf("err = %q, want it to name the placeholder and the store it needs", m.errBar.text)
	}
}

func TestOpenForkRejectsGroup(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("work", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openFork()
	if m.errBar.text != "select a session to fork" {
		t.Fatalf("group fork error = %q", m.errBar.text)
	}
}

// Tools split into two fork shapes: those that mint their own conversation id
// (codex) leave {new_id} out, so the fork starts without one until the
// session store captures it; those that take an agent-chosen id (grok,
// claude) include {new_id} and the fork launches with it already set.
func TestForkAgentSessionIDFollowsNewIDPlaceholder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		tool      string
		forkCmd   string
		wantNewID bool
	}{
		{"codex_session_store", "codex", "true {id}; cat", false},
		{"grok_id_flag", "grok", "true {id} {new_id}; cat", true},
		{"pi_id_flag", "pi", "true {id} {new_id}; cat", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			dir := t.TempDir()
			source := store.Session{
				ID:             "source-" + tc.tool,
				Name:           "source",
				Tool:           tc.tool,
				Cwd:            dir,
				Status:         status.Idle,
				AgentSessionID: "source-conversation",
			}
			if err := m.services.store.CreateSession(source); err != nil {
				t.Fatal(err)
			}
			loadStoredRows(t, m)
			m.selectSessionRow(t, "source")

			tool := m.services.cfg.Tools[tc.tool]
			tool.ForkCommand = tc.forkCmd
			tool.MCP = "none"
			m.services.cfg.Tools[tc.tool] = tool

			m.openFork()
			if m.errBar.text != "" {
				t.Fatalf("openFork error = %q", m.errBar.text)
			}
			openFork(m).SetName("forked")
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(*Model)
			m.applyCmd(t, cmd)
			if !m.onList() || m.errBar.text != "" {
				t.Fatalf("after fork: screen = %v err=%q", m.screenID(), m.errBar.text)
			}

			var forked store.Session
			for _, sess := range m.sessionRows() {
				if sess.Name == "forked" {
					forked = sess
					break
				}
			}
			if forked.ID == "" {
				t.Fatal("forked session not found")
			}
			if tc.wantNewID {
				if forked.AgentSessionID == "" || forked.AgentSessionID == source.AgentSessionID {
					t.Fatalf("forked AgentSessionID = %q, want a fresh id", forked.AgentSessionID)
				}
			} else if forked.AgentSessionID != "" {
				t.Fatalf("forked AgentSessionID = %q, want empty until the session store captures it", forked.AgentSessionID)
			}
		})
	}
}

// opencode's session store makes the fork through opencode's own API, and
// the launch opens the id it answered with.
func TestForkOpencodeOpensTheCopyItsStoreMade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the opencode stand-in is a shell script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\ntest \"$1\" = api || exit 2\necho '{\"data\":{\"id\":\"ses_copy\"}}'\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{ID: "oc-source", Name: "source", Tool: "opencode", Cwd: dir, Status: status.Idle, AgentSessionID: "ses_source"}
	if err := m.services.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")
	argsFile := filepath.Join(t.TempDir(), "fork-args")
	m.services.cfg.Tools["opencode"] = config.Tool{SessionStore: "opencode", ForkCommand: captureArgsCommand(argsFile, "{new_id}"), MCP: "none"}

	m.openFork()
	if m.errBar.text != "" {
		t.Fatalf("openFork error = %q", m.errBar.text)
	}
	openFork(m).SetName("forked")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !m.onList() || m.errBar.text != "" {
		t.Fatalf("after fork: screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "forked" {
			forked = sess
		}
	}
	if forked.AgentSessionID != "ses_copy" {
		t.Fatalf("forked AgentSessionID = %q, want the id opencode answered with", forked.AgentSessionID)
	}
	if got := readWhenWritten(t, argsFile); strings.TrimSpace(got) != "ses_copy" {
		t.Fatalf("fork launched with %q, want ses_copy", got)
	}
}

// gemini forks by handing `--session-file` the source's on-disk session file;
// gemini imports it as a brand-new conversation, so the fork launches with no
// conversation id and the resolver's path must reach the command line intact.
func TestForkGeminiResolvesSessionFile(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "gemini-source",
		Name:           "source",
		Tool:           "gemini",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "gemini-conversation",
	}
	if err := m.services.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")

	const sourceFile = "/store/gemini/session-source.jsonl"
	previousResolver := uiapply.ForkSessionFile
	uiapply.ForkSessionFile = func(sessionStore, id string) (string, error) {
		if sessionStore != "gemini" {
			t.Errorf("resolver session store = %q, want gemini", sessionStore)
		}
		if id != source.AgentSessionID {
			t.Errorf("resolver id = %q, want %q", id, source.AgentSessionID)
		}
		return sourceFile, nil
	}
	t.Cleanup(func() { uiapply.ForkSessionFile = previousResolver })

	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.services.cfg.Tools["gemini"]
	tool.ForkCommand = captureArgsCommand(argsFile, "{session_file}")
	tool.SessionStore = "gemini"
	tool.MCP = "none"
	m.services.cfg.Tools["gemini"] = tool

	m.openFork()
	if m.errBar.text != "" {
		t.Fatalf("openFork error = %q", m.errBar.text)
	}
	openFork(m).SetName("forked")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !m.onList() || m.errBar.text != "" {
		t.Fatalf("after fork: screen = %v err=%q", m.screenID(), m.errBar.text)
	}

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "forked" {
			forked = sess
			break
		}
	}
	if forked.ID == "" {
		t.Fatal("forked session not found")
	}
	if forked.AgentSessionID != "" {
		t.Fatalf("forked AgentSessionID = %q, want empty until the gemini store captures it", forked.AgentSessionID)
	}

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	var err error
	for time.Now().Before(deadline) {
		raw, err = os.ReadFile(argsFile)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != sourceFile {
		t.Fatalf("session file passed to fork = %q, want %q", got, sourceFile)
	}
}

// A fork whose source file cannot be resolved reports the error and does not
// launch, leaving the session list unchanged.
func TestForkGeminiResolverFailureReportsError(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "gemini-source",
		Name:           "source",
		Tool:           "gemini",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "gemini-conversation",
	}
	if err := m.services.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")

	previousResolver := uiapply.ForkSessionFile
	uiapply.ForkSessionFile = func(_, id string) (string, error) {
		return "", fmt.Errorf("no gemini session file found for conversation %s", id)
	}
	t.Cleanup(func() { uiapply.ForkSessionFile = previousResolver })

	tool := m.services.cfg.Tools["gemini"]
	tool.ForkCommand = "gemini --session-file {session_file}"
	tool.SessionStore = "gemini"
	m.services.cfg.Tools["gemini"] = tool

	before := len(m.sessionRows())
	m.openFork()
	openFork(m).SetName("forked")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "no gemini session file") {
		t.Fatalf("resolver error = %q", m.errBar.text)
	}
	if got := len(m.sessionRows()); got != before {
		t.Fatalf("session rows = %d, want %d (no fork launched)", got, before)
	}
}

// A shell has no fork_command, but saying so names a config field for a
// row that was never going to hold a conversation.
func TestForkRefusesAShellInItsOwnTerms(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	m.openFork()

	if forkOpen(m) {
		t.Fatal("fork should not open on a shell row")
	}
	if !strings.Contains(m.errBar.text, "is a shell") ||
		!strings.Contains(m.errBar.text, "no conversation to fork") {
		t.Fatalf("err = %q, want it to name the row as a shell", m.errBar.text)
	}
	if strings.Contains(m.errBar.text, "fork_command") {
		t.Fatalf("err = %q should not name a config field", m.errBar.text)
	}
}

// forkInSourceModel sets up a resting source whose tool forks from inside
// itself, the way muse's /fork does, with the fork read back from a muse store.
func forkInSourceModel(t *testing.T) (*Model, store.Session, string) {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := os.MkdirAll(filepath.Join(dataHome, "muse", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := buildModel(t)
	source := spawnedSession(t, m, "ready-tool")
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !(source.Status == status.Finished || source.Status == status.Idle || source.Status == status.Errored); {
		if time.Now().After(deadline) {
			t.Fatalf("source never came to rest: %q", source.Status)
		}
		time.Sleep(50 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
		var err error
		if source, err = m.services.store.Get(source.ID); err != nil {
			t.Fatal(err)
		}
	}
	m.selectSessionRow(t, source.Name)

	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.services.cfg.Tools[source.Tool]
	tool.SessionStore = "muse"
	tool.ForkKeys = "/fork"
	tool.ForkCommand = captureArgsCommand(argsFile, "{new_id}")
	m.services.cfg.Tools[source.Tool] = tool
	return m, source, argsFile
}

func writeMuseFork(forkID, sourceID string) error {
	path := filepath.Join(os.Getenv("XDG_DATA_HOME"), "muse", "sessions", forkID, "session.jsonl")
	record := `{"payload_type":"session.fork.created","payload":{"fork_session_id":"` + forkID +
		`","source_session_id":"` + sourceID + `"}}` + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(record), 0o600)
}

// Plays the tool's side of /fork: once the keys land in the source pane, the
// store records a fork of the source conversation.
func recordForkWhenTyped(t *testing.T, m *Model, source store.Session, forkID string) {
	t.Helper()
	go func() {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			pane, err := m.services.tmux.CapturePane(source.ID)
			if err != nil || !strings.Contains(pane, "/fork") {
				continue
			}
			_ = writeMuseFork(forkID, source.AgentSessionID)
			return
		}
	}()
}

// An earlier fork still in use has a fresh log too; only the fork the keys
// made may open.
func TestForkInSourceOpensTheForkTheSourceMade(t *testing.T) {
	m, source, argsFile := forkInSourceModel(t)
	if err := writeMuseFork("earlier-fork", source.AgentSessionID); err != nil {
		t.Fatal(err)
	}
	recordForkWhenTyped(t, m, source, "fork-conversation")

	m.openFork()
	openFork(m).SetName("child fork")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !forkOpen(m) {
		t.Fatalf("after submit: screen = %v, err = %q; the dialog stays open while the fork runs", m.screenID(), m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if !m.onList() {
		t.Fatalf("after completion: screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if m.errBar.text != "" {
		t.Fatalf("fork error = %q", m.errBar.text)
	}

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "child fork" {
			forked = sess
		}
	}
	if forked.ID == "" || forked.AgentSessionID != "fork-conversation" {
		t.Fatalf("forked session = %+v, want it bound to fork-conversation", forked)
	}
	deadline := time.Now().Add(2 * time.Second)
	raw, err := os.ReadFile(argsFile)
	for err != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		raw, err = os.ReadFile(argsFile)
	}
	if err != nil || strings.TrimSpace(string(raw)) != "fork-conversation" {
		t.Fatalf("fork command args = %q, err = %v; want fork-conversation", raw, err)
	}
}

// Typing into a source mid-turn would land the keys in its reply stream.
func TestForkInSourceRefusesABusySource(t *testing.T) {
	m, source, _ := forkInSourceModel(t)
	if err := m.services.store.UpdateStatus(source.ID, status.Working); err != nil {
		t.Fatal(err)
	}
	m.openFork()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "fork it once it rests") {
		t.Fatalf("busy source error = %q", m.errBar.text)
	}
	pane, err := m.services.tmux.CapturePane(source.ID)
	if err != nil || strings.Contains(pane, "/fork") {
		t.Fatalf("pane = %q, err = %v; want nothing typed into a busy source", pane, err)
	}
}

// Without the forks the source already had, any of them could pass for the
// new one, so the keys are never typed.
func TestForkInSourceRefusesAnUnreadableStore(t *testing.T) {
	m, source, _ := forkInSourceModel(t)
	if err := os.RemoveAll(filepath.Join(os.Getenv("XDG_DATA_HOME"), "muse")); err != nil {
		t.Fatal(err)
	}
	m.openFork()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "cannot read the forks") {
		t.Fatalf("unreadable store error = %q", m.errBar.text)
	}
	pane, err := m.services.tmux.CapturePane(source.ID)
	if err != nil || strings.Contains(pane, "/fork") {
		t.Fatalf("pane = %q, err = %v; want nothing typed", pane, err)
	}
}

func forkOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.ForkSurface](m)
	return ok
}

// openFork is the fork card on the stack; a test that edits one without
// opening it fails here.
func openFork(m *Model) *uidialogs.ForkSurface {
	s, ok := surfaceOf[*uidialogs.ForkSurface](m)
	if !ok {
		panic("no fork is open")
	}
	return s
}

func openForkFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "alpha")
	alpha := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(alpha.ID, "alpha-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == alpha.ID {
			m.workspace.sessions[i].AgentSessionID = "alpha-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "alpha")
	m.openFork()
	if !forkOpen(m) {
		t.Fatalf("test setup did not open the fork, err %q", m.errBar.text)
	}
	return m
}

// The name is a draft: a click off the card keeps it, esc closes.
func TestForkKeepsItsDraftAndEscCloses(t *testing.T) {
	m := openForkFor(t)
	if mustTop(t, m).Dismissible() {
		t.Fatal("a fork holds a typed name and must not close on a stray click")
	}
	if m = clickBackdrop(t, m); !forkOpen(m) {
		t.Fatal("a backdrop click closed the draft")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if forkOpen(m) || !m.onList() {
		t.Fatalf("esc: open=%t screen = %v", forkOpen(m), m.screenID())
	}
}

// A completion for a card that was popped is dropped: it neither closes
// anything nor focuses the new row, and the error still reaches the status.
func TestStaleForkCompletionIsDropped(t *testing.T) {
	m := openForkFor(t)
	request := uiapply.ForkEffect{ForkRequest: uiapply.ForkRequest{Source: openFork(m).Root().(forkRoot).source, Name: openFork(m).Name(), Gen: openFork(m).Gen()}}
	m.Pop()
	m.openFork()
	m.applyFork(request, uiapply.ForkEffectResult{}, nil)
	if !forkOpen(m) {
		t.Fatal("a completion from a popped fork closed the reopened card")
	}
}

// forkFileTool points the source's fork command at a file, so a test can
// read exactly what the launched fork received without reading a pane.
func forkFileTool(t *testing.T, m *Model, name string) string {
	t.Helper()
	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.services.cfg.Tools[name]
	tool.ForkCommand = "printf '%s\\n' {id} {new_id} {name} > " + tmux.ShellQuote(argsFile) + "; cat"
	m.services.cfg.Tools[name] = tool
	return argsFile
}

func submitForkDialog(t *testing.T, m *Model, name string) (*Model, tea.Cmd) {
	t.Helper()
	m.openFork()
	if !forkOpen(m) {
		t.Fatalf("fork dialog did not open: err=%q", m.errBar.text)
	}
	openFork(m).SetName(name)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	return m, cmd
}

func forkRow(m *Model, name string) (store.Session, bool) {
	for _, sess := range m.sessionRows() {
		if sess.Name == name {
			return sess, true
		}
	}
	return store.Session{}, false
}

// The dialog submit must not read the store, resolve files, type keys or
// launch: only a queued, frozen request may follow it.
func TestForkSubmitDefersWorkOffTheUpdatePath(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	argsFile := forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "child fork")
	if !forkOpen(m) {
		t.Fatalf("after submit: screen = %v, want the dialog to stay open while work runs", m.screenID())
	}
	if _, ok := forkRow(m, "child fork"); ok {
		t.Fatal("submit launched the fork on the update path")
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatal("submit ran the fork command on the update path")
	}
	if m.effects.Main.Active == nil {
		t.Fatal("no active effect, want the queued fork request")
	}
	if _, ok := m.effects.Main.Active.Request.(uiapply.ForkEffect); !ok {
		t.Fatalf("active effect = %T, want a fork effect", m.effects.Main.Active.Request)
	}

	m.applyCmd(t, cmd)
	if !m.onList() || m.errBar.text != "" {
		t.Fatalf("after completion: screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	if forked, ok := forkRow(m, "child fork"); !ok || forked.AgentSessionID == "" || forked.AgentSessionID == source.AgentSessionID {
		t.Fatalf("forked row = %+v", forked)
	}
}

// A completion must apply the durable fork row without replacing a newer
// dialog, moving the selection, or clearing its status text.
func TestForkCompletionDoesNotStealNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	for _, name := range []string{"alpha", "beta"} {
		m.selectSessionRow(t, name)
		sess := railSelectedSession(m)
		if err := m.services.store.SetAgentSessionID(sess.ID, sess.Name+"-conversation"); err != nil {
			t.Fatal(err)
		}
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == sess.ID {
				m.workspace.sessions[i].AgentSessionID = sess.Name + "-conversation"
			}
		}
		m.rebuildRows()
		m.selectSessionRow(t, name)
	}

	m.selectSessionRow(t, "alpha")
	alpha := railSelectedSession(m)
	m, alphaCmd := submitForkDialog(t, m, "alpha fork")
	if openFork(m).Root().(forkRoot).source.ID != alpha.ID {
		t.Fatalf("fork submitted from %q, want alpha %q", openFork(m).Root().(forkRoot).source.ID, alpha.ID)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.selectSessionRow(t, "beta")
	beta := railSelectedSession(m)
	m.openFork()
	if !forkOpen(m) || openFork(m).Root().(forkRoot).source.ID != beta.ID {
		t.Fatalf("newer dialog = %v / %q, want beta's fork dialog", m.screenID(), openFork(m).Root().(forkRoot).source.ID)
	}

	m.applyCmd(t, alphaCmd)
	if !forkOpen(m) || openFork(m).Root().(forkRoot).source.ID != beta.ID {
		t.Fatalf("completion replaced the newer dialog: screen = %v source=%q", m.screenID(), openFork(m).Root().(forkRoot).source.ID)
	}
	if got := railSelectedSession(m); got.ID != beta.ID {
		t.Fatalf("completion moved the selection to %q, want beta kept", got.Name)
	}
	if m.errBar.text != "" {
		t.Fatalf("completion clobbered status text: %q", m.errBar.text)
	}
	if _, ok := forkRow(m, "alpha fork"); !ok {
		t.Fatal("durable fork row not applied after the newer dialog opened")
	}
}

// A window update must return while the fork worker is blocked mid-launch.
func TestForkUpdateReturnsWhileWorkerIsBlocked(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "conv-1"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")
	tool := m.services.cfg.Tools["claude"]
	tool.SessionStore = "gemini"
	tool.ForkCommand = "cat {session_file}; cat"
	m.services.cfg.Tools["claude"] = tool

	started := make(chan struct{})
	release := make(chan struct{})
	var signal sync.Once
	original := uiapply.ForkSessionFile
	uiapply.ForkSessionFile = func(sessionStore, id string) (string, error) {
		signal.Do(func() { close(started) })
		<-release
		return "/fork/session.jsonl", nil
	}
	defer func() {
		uiapply.ForkSessionFile = original
	}()

	m, cmd := submitForkDialog(t, m, "file fork")
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-started
	updated := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on the running fork")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)
	if !m.onList() || m.errBar.text != "" {
		t.Fatalf("after completion: screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	if _, ok := forkRow(m, "file fork"); !ok {
		t.Fatal("fork row missing after the blocked worker completed")
	}
}

// The frozen request must own copies of the source's mutable fields.
func TestForkRequestDeepCopiesMutableSourceFields(t *testing.T) {
	m := &Model{}
	source := store.Session{ID: "captured", PendingInputs: []string{"first"}, RelaunchSnapshot: map[string]int64{"conversation": 1}}
	m.enqueueEffect(uiapply.ForkEffect{ForkRequest: uiapply.ForkRequest{Source: source, Name: "child"}}, 0, false)
	source.PendingInputs[0] = "changed"
	source.RelaunchSnapshot["conversation"] = 2
	request := m.effects.Main.Pending[0].Request.(uiapply.ForkEffect)
	if request.Source.PendingInputs[0] != "first" || request.Source.RelaunchSnapshot["conversation"] != 1 {
		t.Fatal("request retained mutable source aliases")
	}
}

// A fork accepted before quit drains to a durable row; a fork queued after
// quit is refused, and a late command after StopEffects touches nothing.
func TestForkQuitDrainsAcceptedJobAndRefusesLate(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, _ = submitForkDialog(t, m, "drained fork")
	m.requestQuit()
	m.drainEffects(t)
	if _, ok := forkRow(m, "drained fork"); !ok {
		t.Fatal("quit discarded the accepted fork")
	}
	if len(m.effects.Main.Pending) != 0 {
		t.Fatal("quit accepted new user work")
	}
	if _, ok := m.nextEffectCmd()().(tea.QuitMsg); !ok {
		t.Fatal("drained queue did not quit")
	}

	m2 := buildModel(t)
	createSession(t, m2, "late", t.TempDir(), "")
	forkFileTool(t, m2, "claude")
	m2.selectSessionRow(t, "late")
	late := railSelectedSession(m2)
	if err := m2.services.store.SetAgentSessionID(late.ID, "late-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m2.workspace.sessions {
		if m2.workspace.sessions[i].ID == late.ID {
			m2.workspace.sessions[i].AgentSessionID = "late-conversation"
		}
	}
	m2.rebuildRows()
	m2.selectSessionRow(t, "late")
	m2.openFork()
	openFork(m2).SetName("late fork")
	_, lateCommand := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2.StopEffects()
	msg := lateCommand().(uieffects.Completed)
	if msg.Err != uieffects.ErrStopped {
		t.Fatalf("late command error=%v", msg.Err)
	}
	if _, ok := forkRow(m2, "late fork"); ok {
		t.Fatal("late command launched the fork")
	}
}

// Running a fork command twice and delivering its completion twice must
// produce exactly one durable row.
func TestForkDuplicateCompletionIsIdempotent(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "twin fork")
	msg := cmd()
	cmd()
	m.applyTestMsg(t, msg)
	m.applyTestMsg(t, msg)
	rows := 0
	for _, sess := range m.sessionRows() {
		if sess.Name == "twin fork" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("fork rows = %d, want one durable row", rows)
	}
	if !m.onList() || m.errBar.text != "" {
		t.Fatalf("after duplicate completion: screen = %v err=%q", m.screenID(), m.errBar.text)
	}
}

// A launch refusal must keep the captured request for the install's retry,
// not a closure that re-reads model state.
func TestForkLaunchFailureRetainsCapturedRequest(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")
	tool := m.services.cfg.Tools["claude"]
	tool.ForkCommand = "definitely-missing-fork-cli {id}"
	m.services.cfg.Tools["claude"] = tool

	m, cmd := submitForkDialog(t, m, "failed fork")
	m.applyCmd(t, cmd)
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint dialog", m.screenID())
	}
	request, ok := hintFix(m).Retry.(uiapply.ForkEffect)
	if !ok {
		t.Fatal("launch hint holds no captured fork request to re-dispatch")
	}
	if request.Source.ID != source.ID || request.Name != "failed fork" {
		t.Fatalf("captured request = %+v, want the frozen source and name", request)
	}
	if _, ok := forkRow(m, "failed fork"); ok {
		t.Fatal("refused launch left a durable row")
	}
}

// The worker re-reads the source by manager id, so a directory that died
// after the dialog opened is refused at execution time, not submit time.
func TestForkRefusesASourceDirectoryThatDied(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not remove a live pane's working directory")
	}
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "source", dir, "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "gone fork")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "working directory no longer exists") {
		t.Fatalf("err = %q, want the stale directory refusal", m.errBar.text)
	}
	if _, ok := forkRow(m, "gone fork"); ok {
		t.Fatal("fork launched into a missing directory")
	}
}

// A source that restarts while the fork is queued mints a new conversation;
// the re-read validates liveness, and the changed captured identity is
// refused instead of forking the replacement conversation.
func TestForkRefusesASourceThatRestartedWhileQueued(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "restarted fork")
	if err := m.services.store.SetAgentSessionID(source.ID, "replacement-conversation"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetAgentLaunchedAt(source.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "changed while the fork was in the queue") {
		t.Fatalf("err = %q, want the captured-identity refusal", m.errBar.text)
	}
	if !forkOpen(m) {
		t.Fatalf("screen = %v, want the dialog left open to resubmit", m.screenID())
	}
	if _, ok := forkRow(m, "restarted fork"); ok {
		t.Fatal("fork launched from a restarted source")
	}
}

// Reopening a fork on the same source mints a new dialog; an in-flight
// completion of the older one must apply its durable row without closing
// the newer dialog.
func TestForkCompletionAfterSameSourceReopenKeepsNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, firstCmd := submitForkDialog(t, m, "first fork")
	firstGen := openFork(m).Gen()
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.selectSessionRow(t, "source")
	m.openFork()
	if openFork(m).Gen() == firstGen {
		t.Fatal("reopen did not mint a new dialog generation")
	}
	if openFork(m).Root().(forkRoot).source.ID != source.ID {
		t.Fatalf("reopened dialog = %q, want the same source %q", openFork(m).Root().(forkRoot).source.ID, source.ID)
	}

	m.applyCmd(t, firstCmd)
	if !forkOpen(m) || openFork(m).Gen() == firstGen {
		t.Fatalf("completion closed the reopened dialog: screen = %v gen=%d", m.screenID(), openFork(m).Gen())
	}
	if m.errBar.text != "" {
		t.Fatalf("completion clobbered status text: %q", m.errBar.text)
	}
	if got := railSelectedSession(m); got.ID != source.ID {
		t.Fatalf("completion moved the selection to %q, want %q kept", got.Name, source.Name)
	}
	if _, ok := forkRow(m, "first fork"); !ok {
		t.Fatal("durable fork row not applied after the same-source reopen")
	}
}

// A launch failure after the source's own fork was recorded must not retype
// the fork keys on retry: the recorded fork is reused, the source is not
// forked a second time.
func TestForkKeysRetryDoesNotForkSourceTwice(t *testing.T) {
	m, source, argsFile := forkInSourceModel(t)
	_ = argsFile
	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = "definitely-missing-fork-cli {new_id}"
	m.services.cfg.Tools[source.Tool] = tool

	writeOnce := "fork-conversation"
	writeTwice := "second-fork"
	driver := m.services.tmux
	stop := make(chan struct{})
	done := make(chan struct{})
	defer func() { close(stop); <-done }()
	go func() {
		defer close(done)
		typed := 0
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			select {
			case <-stop:
				return
			default:
			}
			pane, err := driver.CapturePane(source.ID)
			if err != nil {
				continue
			}
			if n := strings.Count(pane, "/fork"); n > typed {
				typed = n
				switch n {
				case 1:
					_ = writeMuseFork(writeOnce, source.AgentSessionID)
				case 2:
					_ = writeMuseFork(writeTwice, source.AgentSessionID)
				}
			}
		}
	}()

	m.openFork()
	openFork(m).SetName("child fork")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint dialog for the missing binary", m.screenID())
	}
	request, ok := hintFix(m).Retry.(uiapply.ForkEffect)
	if !ok || request.KnownForkID != writeOnce {
		t.Fatalf("captured retry = %+v, want it to reuse the recorded fork %q", request, writeOnce)
	}

	// The binary is "installed"; the retry must launch from the recorded
	// fork without typing the source's fork keys a second time.
	tool = m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {new_id}; cat"
	m.services.cfg.Tools[source.Tool] = tool
	m.enqueueEffect(request, 0, false)
	m.drainEffects(t)

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "child fork" {
			forked = sess
		}
	}
	if forked.ID == "" {
		t.Fatalf("retry did not launch the fork: err=%q", m.errBar.text)
	}
	if forked.ID != request.ChildID {
		t.Fatalf("retry changed child identity: %s != %s", forked.ID, request.ChildID)
	}
	if forked.AgentSessionID != writeOnce {
		t.Fatalf("forked conversation = %q, want the recorded %q; the keys were typed again", forked.AgentSessionID, writeOnce)
	}
}

// A fork must stay starting, which attention excludes, so a completed
// launch clears the status filter the way the keyboard path did.
func TestForkCompletionClearsStatusFilter(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m.rail.SetFilteringAttention(true)
	m, cmd := submitForkDialog(t, m, "filtered fork")
	m.applyCmd(t, cmd)
	if m.rail.FilteringAttention() {
		t.Fatal("attention filter still on, want it cleared so the starting fork is visible")
	}
	if _, ok := forkRow(m, "filtered fork"); !ok {
		t.Fatal("fork row missing after a filtered list")
	}
}

func TestForkRepeatedSubmitKeepsOneAcceptedJob(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueFork(uiapply.ForkEffect{ForkRequest: uiapply.ForkRequest{Source: store.Session{ID: "source"}, Name: "child", Gen: 1}})
	m.queueFork(uiapply.ForkEffect{ForkRequest: uiapply.ForkRequest{Source: store.Session{ID: "source"}, Name: "child", Gen: 1}})
	if len(m.effects.Main.Pending) != 1 {
		t.Fatal("repeat submit accepted a second fork")
	}
}
