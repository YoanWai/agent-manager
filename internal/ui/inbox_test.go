package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// queueMessage puts one message in a session's inbox the way the MCP
// send_session tool does.
func queueMessage(t *testing.T, m *Model, targetID, body string) int64 {
	t.Helper()
	id, err := m.store.Enqueue(store.InboxMessage{
		SessionID:   targetID,
		SenderID:    "sender01",
		SenderName:  "payments-fix",
		Body:        body,
		Fingerprint: body,
		SentAt:      time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return id
}

func spawnedSession(t *testing.T, m *Model, tool string) store.Session {
	t.Helper()
	if err := m.spawnSession(tool, "worker", t.TempDir(), "", "", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// A dialog leaves the tool's input line drawn underneath it, so the
// activity region reports ready while typing would answer the dialog.
// The rule pass is what tells the two apart.
func TestInboxHoldsAMessageWhileTheAgentSitsOnADialog(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	queueMessage(t, m, sess.ID, "rebase on main")

	dialog := "Do you want to proceed?\n  1. Yes\n  2. No\n↑/↓ to select, Enter to confirm\n❯ "
	if _, ready := m.poller.engine.ActivityRegion(sess.Tool, dialog); !ready {
		t.Fatal("fixture no longer reproduces the hazard: the region must read ready")
	}
	if err := m.poller.maybeDeliverInbox(sess, dialog, status.Waiting, true); err != nil {
		t.Fatalf("maybeDeliverInbox: %v", err)
	}
	queued, err := m.store.QueuedCount(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatal("a message was typed into an approval dialog")
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, "rebase on main") {
		t.Fatalf("message reached a pane showing a dialog:\n%s", pane)
	}
}

func TestInboxHoldsAMessageWhileTheAgentIsWorking(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	queueMessage(t, m, sess.ID, "rebase on main")

	if err := m.poller.maybeDeliverInbox(sess, "❯ ", status.Working, true); err != nil {
		t.Fatalf("maybeDeliverInbox: %v", err)
	}
	if queued, _ := m.store.QueuedCount(sess.ID); queued != 1 {
		t.Fatal("a message was delivered mid-turn")
	}
	// A dead agent cannot read anything either.
	if err := m.poller.maybeDeliverInbox(sess, "❯ ", status.Idle, false); err != nil {
		t.Fatalf("maybeDeliverInbox dead: %v", err)
	}
	if queued, _ := m.store.QueuedCount(sess.ID); queued != 1 {
		t.Fatal("a message was delivered to a dead agent")
	}
}

func TestInboxDeliversToARestingAgentWithItsSenderNamed(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	id := queueMessage(t, m, sess.ID, "rebase on main")

	if err := m.poller.maybeDeliverInbox(sess, "❯ ", status.Idle, true); err != nil {
		t.Fatalf("maybeDeliverInbox: %v", err)
	}
	if queued, _ := m.store.QueuedCount(sess.ID); queued != 0 {
		t.Fatal("message was not delivered to a resting agent")
	}
	state, err := m.store.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.DeliveredAt.IsZero() {
		t.Fatalf("delivery was not recorded: %+v", state)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(pane, "rebase on main") {
			// The envelope wraps at the pane width, and tmux wraps without
			// inserting anything, so the unwrapped text is the joined rows.
			flat := strings.ReplaceAll(pane, "\n", "")
			for _, want := range []string{"not from the user", "payments-fix", "send_session"} {
				if !strings.Contains(flat, want) {
					t.Fatalf("envelope is missing %q:\n%s", want, pane)
				}
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	pane, _ := m.tmux.CapturePane(sess.ID)
	t.Fatalf("message never reached the pane:\n%s", pane)
}

// A claim with no delivery is a manager that died mid-send. Whether the
// text reached the pane is unknowable, so it is retired rather than risk
// running the same instruction twice.
func TestInboxRetiresAMessageItCannotProveWasDelivered(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	id := queueMessage(t, m, sess.ID, "rebase on main")
	if claimed, err := m.store.ClaimMessage(id, time.Now()); err != nil || !claimed {
		t.Fatalf("claim: %v, claimed=%v", err, claimed)
	}

	err := m.poller.maybeDeliverInbox(sess, "❯ ", status.Idle, true)
	if err == nil || !strings.Contains(err.Error(), "unconfirmed message") {
		t.Fatalf("reconcile error = %v", err)
	}
	if queued, _ := m.store.QueuedCount(sess.ID); queued != 0 {
		t.Fatal("an unconfirmed message was left to be retried")
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, "rebase on main") {
		t.Fatalf("unconfirmed message was resent:\n%s", pane)
	}
}
