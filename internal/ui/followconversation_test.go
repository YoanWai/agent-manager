package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// followingPoller is a poller over one muse row whose launch recorded this
// test process as its agent.
func followingPoller(t *testing.T, launch int64) (*poller, store.Session) {
	t.Helper()
	p, sess := newTestPollerWithSession(t)
	p.reportStyles = map[string]string{"codex": "muse"}
	if err := os.MkdirAll(p.hooks.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	record := fmt.Sprintf("%d %d /dev/ttys001\n", os.Getpid(), launch)
	if err := os.WriteFile(p.hooks.AgentFile(sess.ID), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	held := filepath.Join(t.TempDir(), "muse", "sessions", "01a1183b-cbc5.json")
	if err := os.MkdirAll(filepath.Dir(held), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(held)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return p, sess
}

func TestPollerFollowsTheConversationTheAgentHolds(t *testing.T) {
	p, sess := followingPoller(t, 0)
	if err := p.followConversation(&sess, true, false); err != nil {
		t.Fatal(err)
	}
	stored, err := p.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.AgentSessionID != "01a1183b-cbc5" || stored.AgentSessionID != "01a1183b-cbc5" {
		t.Fatalf("row = %q, stored %q; want the held conversation", sess.AgentSessionID, stored.AgentSessionID)
	}
}

// A record from a launch the row has since moved past names an agent that
// no longer speaks for it.
func TestPollerIgnoresAnAgentFromAnotherLaunch(t *testing.T) {
	p, sess := followingPoller(t, store.LaunchStamp(time.Unix(1700000000, 0)))
	if err := p.followConversation(&sess, true, false); err != nil {
		t.Fatal(err)
	}
	if stored, err := p.store.Get(sess.ID); err != nil || stored.AgentSessionID != "" {
		t.Fatalf("stored = %q, %v; want the row left alone", stored.AgentSessionID, err)
	}
}

// A row whose agent record cannot be read must not stop the pass for every
// other row, and a failure that lasts is reported once.
func TestPollerSkipsARowWhoseConversationCannotBeRead(t *testing.T) {
	m := buildModel(t)
	m.poller.reportStyles = map[string]string{"claude": "hermes"}
	socket := m.tmux.SocketPath()
	for _, id := range []string{"broken01", "healthy1"} {
		if err := m.store.CreateSession(store.Session{ID: id, Name: id, Tool: "claude", Cwd: t.TempDir(), Status: status.Idle, TmuxSocket: socket}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(m.hooks.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.hooks.AgentFile("broken01"), []byte("not a record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := m.poller.refreshOnce()
	msg, ok := result.(refreshMsg)
	if !ok {
		t.Fatalf("the pass stopped at the broken row: %v", result)
	}
	if !strings.Contains(msg.followWarning, "broken01") {
		t.Fatalf("followWarning = %q, want the broken row named", msg.followWarning)
	}
	for _, id := range []string{"broken01", "healthy1"} {
		if sess, err := m.store.Get(id); err != nil || sess.Status != status.Dead {
			t.Fatalf("%s = %q, %v; want the pass to mark it dead", id, sess.Status, err)
		}
	}
	if again, ok := m.poller.refreshOnce().(refreshMsg); !ok || again.followWarning != "" {
		t.Fatalf("second pass = %#v; want the same failure left unreported", again.followWarning)
	}
}

// Grok's log outlives the agent, so a switch made while no manager was
// watching still moves a row whose pane has since died.
func TestPollerFollowsADeadAgentsSwitchFromGroksLog(t *testing.T) {
	p, sess := newTestPollerWithSession(t)
	p.reportStyles = map[string]string{"codex": "grok"}
	if err := os.MkdirAll(p.hooks.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	const quitPID = 999999
	record := fmt.Sprintf("%d %d /dev/ttys001\n", quitPID, store.LaunchStamp(sess.AgentLaunchedAt))
	if err := os.WriteFile(p.hooks.AgentFile(sess.ID), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	if err := os.MkdirAll(filepath.Join(grokHome, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	loggedAt := time.Now().Format(time.RFC3339Nano)
	log := fmt.Sprintf("{\"ts\":%q,\"pid\":%d,\"sid\":\"019a1c2d-0000-7000-8000-000000000001\"}\n{\"ts\":%q,\"pid\":%d,\"sid\":\"019a1c2d-0000-7000-8000-000000000002\"}\n", loggedAt, quitPID, loggedAt, quitPID)
	if err := os.WriteFile(filepath.Join(grokHome, "logs", "unified.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.followConversation(&sess, false, true); err != nil {
		t.Fatal(err)
	}
	stored, err := p.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentSessionID != "019a1c2d-0000-7000-8000-000000000002" {
		t.Fatalf("stored %q, want the conversation grok last logged for the agent", stored.AgentSessionID)
	}
}
