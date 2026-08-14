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
	state, err := m.store.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.DroppedAt.IsZero() {
		t.Fatalf("an unconfirmed message was retired as delivered: %+v", state)
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, "rebase on main") {
		t.Fatalf("unconfirmed message was resent:\n%s", pane)
	}
}

// A send that fails leaves a claimed row nothing may retype, so the queue
// looks the same as it does after a delivery. Recording the drop is what
// keeps message_status from telling the sender its message was typed in.
func TestInboxRecordsAMessageItCouldNotTypeAsDropped(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	id := queueMessage(t, m, sess.ID, "rebase on main")
	// The pane is gone, so the send tmux is asked to make cannot land. The
	// poller is told the agent is alive, which is what a session dying
	// between the capture and the send looks like.
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill pane: %v", err)
	}

	err := m.poller.maybeDeliverInbox(sess, "❯ ", status.Idle, true)
	if err == nil || !strings.Contains(err.Error(), "dropped a message") {
		t.Fatalf("a failed send reported %v", err)
	}
	state, err := m.store.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.DroppedAt.IsZero() {
		t.Fatalf("the drop was not recorded: %+v", state)
	}
	if queued, _ := m.store.QueuedCount(sess.ID); queued != 0 {
		t.Fatal("a dropped message was left to be retried")
	}
}

// The pane and the derived status are captured once per poll. A launch
// input typed during that poll leaves both describing the moment before
// it was sent, so a message delivered on the same tick would land on an
// agent that is already starting a turn.
func TestInboxWaitsAPollAfterALaunchInputIsTyped(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("send-tool", "", t.TempDir(), "", "", true, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.PendingInputs) == 0 {
		t.Fatal("fixture no longer reproduces the hazard: the spawn must queue a launch input")
	}
	queueMessage(t, m, sess.ID, "rebase on main")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.applyCmd(t, m.refreshCmd())
		current, err := m.store.Get(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(current.PendingInputs) > 0 {
			continue
		}
		// The launch input has gone out. On that same poll the message must
		// still be queued; it may only leave on a later one.
		queued, err := m.store.QueuedCount(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if queued == 0 {
			t.Fatal("message was delivered on the same poll that typed the launch input")
		}
		return
	}
	t.Fatal("the launch input was never delivered")
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
	if err := m.store.SetArchived(ids["stranded"], true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	queueMessage(t, m, ids["stranded"], "rebase on main")
	queueMessage(t, m, ids["stranded"], "then push")

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())

	if got := m.queuedMessages[ids["stranded"]]; got != 2 {
		t.Fatalf("queued count = %d want 2: %v", got, m.queuedMessages)
	}
	if _, listed := m.queuedMessages[ids["quiet"]]; listed {
		t.Fatalf("a session with an empty inbox got a count: %v", m.queuedMessages)
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
	queueMessage(t, m, sess.ID, "rebase on main")
	m.queuedMessages = map[string]int{sess.ID: 1}

	if err := m.poller.maybeDeliverInbox(sess, "❯ ", status.Idle, true); err != nil {
		t.Fatalf("maybeDeliverInbox: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	if count, stale := m.queuedMessages[sess.ID]; stale {
		t.Fatalf("a delivered message left a badge of %d behind", count)
	}
}

// pollUntilQueued runs the poller's own loop until the session's queue is
// the given depth, so delivery goes through the gate a live manager
// applies rather than a hand-picked pane and status.
func pollUntilQueued(t *testing.T, m *Model, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		queued, err := m.store.QueuedCount(sessionID)
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

// settledPane waits for the pane to hold every marker and stop changing,
// since the tool echoes what was typed and then prints it back.
func settledPane(t *testing.T, m *Model, sessionID string, markers ...string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	previous, repeats := "", 0
	for time.Now().Before(deadline) {
		pane, err := m.tmux.CapturePane(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if pane == previous {
			repeats++
		} else {
			repeats = 0
		}
		previous = pane
		if repeats >= 3 && containsAll(pane, markers) {
			return pane
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

// The other inbox tests hand maybeDeliverInbox a pane and a status; these
// two drive the loop that reads both itself, which is the only path a
// live manager takes.
func TestThePollLoopDeliversQueuedMessagesOldestFirst(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "ready-tool")
	first := queueMessage(t, m, sess.ID, "rebase on main")
	second := queueMessage(t, m, sess.ID, "then push the branch")

	pollUntilQueued(t, m, sess.ID, 1)
	head, queued, err := m.store.HeadMessage(sess.ID)
	if err != nil || !queued {
		t.Fatalf("HeadMessage: %v, queued=%v", err, queued)
	}
	if head.ID != second {
		t.Fatalf("the newer message was delivered first: head = %+v", head)
	}

	pollUntilQueued(t, m, sess.ID, 0)
	for _, id := range []int64{first, second} {
		state, err := m.store.Message(id, "sender01")
		if err != nil {
			t.Fatal(err)
		}
		if state.DeliveredAt.IsZero() {
			t.Fatalf("message %d never reached the pane: %+v", id, state)
		}
	}
	settledPane(t, m, sess.ID, "rebase on main", "then push the branch")
}

// A message typed a second time runs the same instruction twice, so the
// polls that follow a delivery must leave the pane exactly as it was.
func TestThePollLoopNeverRetypesADeliveredMessage(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "ready-tool")
	queueMessage(t, m, sess.ID, "rebase on main")
	pollUntilQueued(t, m, sess.ID, 0)
	delivered := settledPane(t, m, sess.ID, "rebase on main")

	for range 5 {
		m.applyCmd(t, m.refreshCmd())
	}
	if again := settledPane(t, m, sess.ID, "rebase on main"); again != delivered {
		t.Fatalf("a poll after the queue drained typed into the pane again:\nbefore:\n%s\nafter:\n%s", delivered, again)
	}
	// Those were live polls rather than ones a shut gate skipped: a gate
	// that closed behind the first delivery would have nothing to retype
	// either, and this test would pass without proving anything.
	queueMessage(t, m, sess.ID, "then push the branch")
	pollUntilQueued(t, m, sess.ID, 0)
}

// A rule that classifies a resting frame must not pin the gate shut: pi
// marks a resumed session idle, and every message to it would wait forever.
func TestInboxDeliversWhenARuleReportsARestingState(t *testing.T) {
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	queueMessage(t, m, sess.ID, "rebase on main")

	resting := "error: something went wrong earlier\n❯ "
	if state, matched := m.poller.engine.RuleMatch(sess.Tool, resting); !matched || state == status.Working || state == status.Waiting {
		t.Fatalf("fixture no longer reproduces the case: rule match = %q, %v", state, matched)
	}
	if err := m.poller.maybeDeliverInbox(sess, resting, status.Idle, true); err != nil {
		t.Fatalf("maybeDeliverInbox: %v", err)
	}
	if queued, _ := m.store.QueuedCount(sess.ID); queued != 0 {
		t.Fatal("a resting rule state blocked delivery for good")
	}
}
