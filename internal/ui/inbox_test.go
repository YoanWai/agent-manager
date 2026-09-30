package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

// queueMessage puts one message in a session's inbox the way the MCP
// send_session tool does.
func queueMessage(t *testing.T, m *Model, targetID, body string) int64 {
	t.Helper()
	id, err := m.services.store.Enqueue(store.InboxMessage{
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
	sess, err := m.services.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// Archiving a session leaves its queue where it was while the poller stops
// visiting the row, so nothing will ever deliver those messages. The count
// rides the same refresh the sessions do and reaches the row as a badge,
// which is the only thing that says the queue is stuck.
func TestRefreshCarriesQueuedCountsToTheRows(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "quiet", dir, "")
	createSession(t, m, "stranded", dir, "")

	ids := map[string]string{}
	for _, sess := range m.sessionRows() {
		ids[sess.Name] = sess.ID
	}
	if err := m.services.store.SetArchived(ids["stranded"], true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	queueMessage(t, m, ids["stranded"], "rebase on main")
	queueMessage(t, m, ids["stranded"], "then push")

	m.rail.showArchived = true
	m.applyCmd(t, m.refreshCmd())

	if got := m.workspace.queuedMessages[ids["stranded"]]; got != 2 {
		t.Fatalf("queued count = %d want 2: %v", got, m.workspace.queuedMessages)
	}
	if _, listed := m.workspace.queuedMessages[ids["quiet"]]; listed {
		t.Fatalf("a session with an empty inbox got a count: %v", m.workspace.queuedMessages)
	}
	rows := railText(t, m)
	if row := rows[lineWith(t, rows, "stranded")]; !strings.Contains(row, "✉2") {
		t.Fatalf("the stranded queue is invisible on its row: %q", row)
	}
}

// The counts are replaced whole rather than merged, so a message that has
// been delivered takes its badge with it on the next refresh.
func TestRefreshReplacesTheQueuedCountsWholesale(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	id := queueMessage(t, m, sess.ID, "rebase on main")
	m.workspace.queuedMessages = map[string]int{sess.ID: 1}

	if err := m.services.store.MarkDelivered(id, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	if count, stale := m.workspace.queuedMessages[sess.ID]; stale {
		t.Fatalf("a delivered message left a badge of %d behind", count)
	}
}

// pollUntilQueued drives the poller's own loop so delivery passes the gate a
// live manager applies rather than a hand-picked pane and status. The budget
// is generous because that gate waits for the tool to echo the message just
// typed, which takes as long as the runner needs.
func pollUntilQueued(t *testing.T, m *Model, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		queued, err := m.services.store.QueuedCount(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if queued == want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("queue held %d messages, want %d", queued, want)
		}
		m.applyCmd(t, m.refreshCmd())
		time.Sleep(20 * time.Millisecond)
	}
}

// settledPane waits for the pane to hold every marker and stop changing.
// The markers come from the tty echo of what was pasted; the fixture tools
// consume their input and print only a fresh prompt.
func settledPane(t *testing.T, m *Model, sessionID string, markers ...string) string {
	t.Helper()
	// Two waits, not one. A paste still landing resets the quiet run, so
	// requiring the markers and the quiet in the same capture can burn
	// the whole deadline on a loaded runner: first wait for every marker
	// to have rendered, then for the pane to stop changing.
	deadline := time.Now().Add(60 * time.Second)
	var previous string
	for {
		pane, err := m.services.tmux.CapturePane(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		previous = pane
		if containsAll(pane, markers) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never showed %v:\n%s", markers, previous)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The quiet run gets its own budget: markers rendering can eat most
	// of the first deadline on a loaded runner, and the few captures the
	// settle needs should not have to fit in whatever is left.
	settleDeadline := time.Now().Add(20 * time.Second)
	repeats := 0
	for time.Now().Before(settleDeadline) {
		pane, err := m.services.tmux.CapturePane(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if pane == previous {
			repeats++
		} else {
			repeats = 0
		}
		previous = pane
		if repeats >= 3 {
			if !containsAll(previous, markers) {
				t.Fatalf("pane settled without %v:\n%s", markers, previous)
			}
			return previous
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pane never settled holding %v:\n%s", markers, previous)
	return ""
}

// The envelope wraps at the pane width, and tmux wraps without inserting
// anything, so the unwrapped text is the joined rows.
func containsAll(pane string, markers []string) bool {
	flat := strings.ReplaceAll(pane, "\n", "")
	for _, marker := range markers {
		if !strings.Contains(flat, marker) {
			return false
		}
	}
	return true
}
