package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	tool.ForkCommand = "tool --fork {id}"
	m.cfg.Tools[source.Tool] = tool
	m.errBar.text = ""
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
