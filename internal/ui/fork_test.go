package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func TestExpandForkCommandQuotesPlaceholders(t *testing.T) {
	got := expandForkCommand("tool --fork {id} --new {new_id} --name {name}", "source", "new", "Sam's fork")
	want := "tool --fork 'source' --new 'new' --name 'Sam'\\''s fork'"
	if got != want {
		t.Fatalf("fork command = %q, want %q", got, want)
	}
}

func TestForkSelectedSessionCreatesNamedSibling(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "source", dir, "work")
	m.selectSessionRow(t, "source")
	source := m.rows[m.cursor].sess
	if err := m.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.sessions {
		if m.sessions[i].ID == source.ID {
			m.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = "printf '%s\\n' {id} {new_id} {name} > " + tmux.ShellQuote(argsFile) + "; cat"
	m.cfg.Tools[source.Tool] = tool

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(*Model)
	if m.mode != modeFork || m.fork.source.ID != source.ID {
		t.Fatalf("fork mode = %v, source = %q", m.mode, m.fork.source.ID)
	}
	m.fork.name.SetValue("child fork")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if m.mode != modeList || m.errBar.text != "" {
		t.Fatalf("after fork: mode=%v err=%q", m.mode, m.errBar.text)
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
	stored, err := m.store.Get(forkedID)
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

func TestForkCopiesManagedWorktreeReference(t *testing.T) {
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
	if err := m.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")
	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {id}; cat"
	m.cfg.Tools[source.Tool] = tool

	m.openFork()
	m.fork.name.SetValue("forked")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
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
	m := buildModel(t)
	repo := seedRepo(t)
	if err := m.spawnSession("claude", "source", repo, "", "", false, true); err != nil {
		t.Fatal(err)
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, err %v", sessions, err)
	}
	source := sessions[0]
	badConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(badConfig, "hooks"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.hooks = hooks.NewManager(badConfig)

	forked := source
	forked.ID = "failed-fork"
	forked.Name = "forked"
	if err := m.launchNewSession(forked, m.cfg.Tools[forked.Tool], "cat", launchOptions{}); err == nil {
		t.Fatal("launch failure was not reported")
	}
	if _, err := os.Stat(source.Cwd); err != nil {
		t.Fatalf("source worktree was removed: %v", err)
	}
}

func TestOpenForkRequiresConfiguredCommandAndConversationID(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := m.rows[m.cursor].sess

	m.openFork()
	if !strings.Contains(m.errBar.text, "no fork_command") {
		t.Fatalf("missing command error = %q", m.errBar.text)
	}

	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = "tool --fork latest"
	m.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "must contain {id}") {
		t.Fatalf("missing placeholder error = %q", m.errBar.text)
	}

	tool.ForkCommand = "tool --fork {id}"
	m.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "no captured conversation id") {
		t.Fatalf("missing id error = %q", m.errBar.text)
	}
}

func TestOpenForkRejectsGroup(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("work", ""); err != nil {
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
// (opencode, codex) leave {new_id} out, so the fork starts without one until
// the session store captures it; those that take an agent-chosen id (grok,
// claude) include {new_id} and the fork launches with it already set.
func TestForkAgentSessionIDFollowsNewIDPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tool      string
		forkCmd   string
		wantNewID bool
	}{
		{"opencode_session_store", "opencode", "true {id}; cat", false},
		{"grok_id_flag", "grok", "true {id} {new_id}; cat", true},
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
			if err := m.store.CreateSession(source); err != nil {
				t.Fatal(err)
			}
			loadStoredRows(t, m)
			m.selectSessionRow(t, "source")

			tool := m.cfg.Tools[tc.tool]
			tool.ForkCommand = tc.forkCmd
			m.cfg.Tools[tc.tool] = tool

			m.openFork()
			if m.errBar.text != "" {
				t.Fatalf("openFork error = %q", m.errBar.text)
			}
			m.fork.name.SetValue("forked")
			updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(*Model)
			m.applyCmd(t, cmd)
			if m.mode != modeList || m.errBar.text != "" {
				t.Fatalf("after fork: mode=%v err=%q", m.mode, m.errBar.text)
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
