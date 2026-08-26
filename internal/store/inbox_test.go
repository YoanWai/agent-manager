package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func message(body string, at time.Time) InboxMessage {
	return InboxMessage{
		SessionID:   "target01",
		SenderID:    "sender01",
		SenderName:  "payments-fix",
		Body:        body,
		Fingerprint: body,
		SentAt:      at,
	}
}

func TestEnqueueDeliverAndAck(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	id, err := st.Enqueue(message("rebase on main", now), DefaultInboxLimits)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if id == 0 {
		t.Fatal("Enqueue returned no id")
	}
	queued, err := st.QueuedCount("target01")
	if err != nil || queued != 1 {
		t.Fatalf("queued = %d, err %v", queued, err)
	}

	head, ok, err := st.HeadMessage("target01")
	if err != nil || !ok {
		t.Fatalf("HeadMessage: %v, ok=%v", err, ok)
	}
	if head.ID != id || head.Body != "rebase on main" || head.SenderName != "payments-fix" {
		t.Fatalf("head = %+v", head)
	}
	if !head.ClaimedAt.IsZero() {
		t.Fatalf("a fresh message must not read as claimed: %+v", head)
	}

	claimed, err := st.ClaimMessage(id, now)
	if err != nil || !claimed {
		t.Fatalf("ClaimMessage: %v, claimed=%v", err, claimed)
	}
	// A second claim is what a racing manager would attempt; only one may win.
	again, err := st.ClaimMessage(id, now)
	if err != nil || again {
		t.Fatalf("second claim = %v, err %v", again, err)
	}
	if err := st.MarkDelivered(id, now); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if _, ok, err := st.HeadMessage("target01"); err != nil || ok {
		t.Fatalf("a delivered message is still queued: ok=%v err=%v", ok, err)
	}

	state, err := st.Message(id, "sender01")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if state.DeliveredAt.IsZero() || !state.ReadAt.IsZero() {
		t.Fatalf("state = %+v", state)
	}
	if err := st.MarkRead("target01", "sender01", now); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	state, err = st.Message(id, "sender01")
	if err != nil || state.ReadAt.IsZero() {
		t.Fatalf("ack did not land: %+v err %v", state, err)
	}
}

func TestEnqueueGuardsAgainstLoops(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	limits := InboxLimits{QueueCap: 3, RateCap: 2, RateWindow: time.Minute, DedupeWindow: time.Minute}

	if _, err := st.Enqueue(message("first", now), limits); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := st.Enqueue(message("first", now), limits); !errors.Is(err, ErrInboxDuplicate) {
		t.Fatalf("repeat of an identical message = %v, want ErrInboxDuplicate", err)
	}
	if _, err := st.Enqueue(message("second", now), limits); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := st.Enqueue(message("third", now), limits); !errors.Is(err, ErrInboxRateLimited) {
		t.Fatalf("third within the window = %v, want ErrInboxRateLimited", err)
	}
	// Past the rate window the sender is allowed again, up to the queue cap.
	later := now.Add(2 * time.Minute)
	if _, err := st.Enqueue(message("third", later), limits); err != nil {
		t.Fatalf("after the window: %v", err)
	}
	if _, err := st.Enqueue(message("fourth", later), limits); !errors.Is(err, ErrInboxFull) {
		t.Fatalf("past the queue cap = %v, want ErrInboxFull", err)
	}
}

func TestInboxIsScopedPerRecipientAndSweptWhenDelivered(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	limits := DefaultInboxLimits

	first, err := st.Enqueue(message("for target", now), limits)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	other := message("for the other one", now)
	other.SessionID = "target02"
	if _, err := st.Enqueue(other, limits); err != nil {
		t.Fatalf("Enqueue other: %v", err)
	}
	counts, err := st.QueuedCounts()
	if err != nil {
		t.Fatalf("QueuedCounts: %v", err)
	}
	if counts["target01"] != 1 || counts["target02"] != 1 {
		t.Fatalf("counts = %v", counts)
	}

	if err := st.MarkDelivered(first, now); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := st.PruneInbox(now.Add(time.Hour)); err != nil {
		t.Fatalf("PruneInbox: %v", err)
	}
	counts, err = st.QueuedCounts()
	if err != nil {
		t.Fatalf("QueuedCounts after prune: %v", err)
	}
	if _, swept := counts["target01"]; swept {
		t.Fatalf("delivered message survived the prune: %v", counts)
	}
	// An undelivered message is the queue itself and must never be swept.
	if counts["target02"] != 1 {
		t.Fatalf("prune took a queued message: %v", counts)
	}
}

// The retention window is the sender's window to read a receipt, and it
// runs from delivery: an undelivered message is never swept, so one can
// wait days behind an agent parked on a dialog and still be brand new to
// its sender when it finally lands.
func TestPruneMeasuresRetentionFromDeliveryRatherThanFromSending(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	id, err := st.Enqueue(message("rebase on main", now.Add(-30*time.Hour)), DefaultInboxLimits)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := st.MarkDelivered(id, now); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := st.PruneInbox(now.Add(-24 * time.Hour)); err != nil {
		t.Fatalf("PruneInbox: %v", err)
	}
	if _, err := st.Message(id, "sender01"); err != nil {
		t.Fatalf("a message delivered a moment ago was swept: %v", err)
	}
	// Its own window passing is what retires it.
	if err := st.PruneInbox(now.Add(time.Hour)); err != nil {
		t.Fatalf("PruneInbox: %v", err)
	}
	if _, err := st.Message(id, "sender01"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a message past its own window survived: %v", err)
	}
}

func TestDeletingASessionTakesItsMessagesBothWays(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	for _, sess := range []Session{
		{ID: "target01", Name: "target", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "sender01", Name: "sender", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := st.CreateSession(sess); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	if _, err := st.Enqueue(message("inbound", now), DefaultInboxLimits); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Either end must clear the message: ids come from a fresh UUID prefix,
	// so a survivor could re-attach to a later session.
	if err := st.Delete("target01"); err != nil {
		t.Fatalf("Delete the recipient: %v", err)
	}
	counts, err := st.QueuedCounts()
	if err != nil {
		t.Fatalf("QueuedCounts: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("messages survived their recipient: %v", counts)
	}
	if _, err := st.Enqueue(message("inbound again", now), DefaultInboxLimits); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := st.Delete("sender01"); err != nil {
		t.Fatalf("Delete the sender: %v", err)
	}
	counts, err = st.QueuedCounts()
	if err != nil {
		t.Fatalf("QueuedCounts: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("messages survived their sender: %v", counts)
	}
}

// MarkDropped retires a message the poller could not prove reached the
// pane: it leaves the queue for good, and a later ack from its recipient
// must not dress it up as read.
func TestMarkDroppedRetiresTheMessageForGood(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	id, err := st.Enqueue(message("rebase on main", now), DefaultInboxLimits)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := st.MarkDropped(id, now); err != nil {
		t.Fatalf("MarkDropped: %v", err)
	}
	if _, ok, err := st.HeadMessage("target01"); err != nil || ok {
		t.Fatalf("a dropped message is still queued: ok=%v err=%v", ok, err)
	}
	state, err := st.Message(id, "sender01")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	if state.DroppedAt.IsZero() || state.DeliveredAt.IsZero() {
		t.Fatalf("dropping did not retire the row: %+v", state)
	}
	if err := st.MarkRead("target01", "sender01", now); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	state, err = st.Message(id, "sender01")
	if err != nil || !state.ReadAt.IsZero() {
		t.Fatalf("an ack reached a dropped message: %+v err %v", state, err)
	}
}

// Two managers starting against one store must not both come away holding
// it: the loser would speak for the same unclaimed sessions from a tmux
// server that cannot see their panes.
func TestClaimPollerHoldsForOneManagerAtATime(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	const first, second = "/tmp/first/agentmgr", "/tmp/second/agentmgr"

	holder, err := st.ClaimPoller(first, now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != first {
		t.Fatalf("holder of an unclaimed store = %q, want %q", holder, first)
	}

	holder, err = st.ClaimPoller(second, now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != first {
		t.Fatalf("holder = %q, want the awake %q to keep it", holder, first)
	}

	holder, err = st.ClaimPoller(second, now.Add(2*PollerHeartbeatStale), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != second {
		t.Fatalf("holder = %q, want %q once the first stopped stamping", holder, second)
	}
	stamp, err := st.Setting(PollerHeartbeatKey)
	if err != nil {
		t.Fatal(err)
	}
	if stamp == "" {
		t.Fatal("a claim should stamp the heartbeat its readers wait on")
	}
}

// Two managers starting at the same instant reach the empty store together,
// which is what a read followed by a separate write cannot survive: both
// would read "unclaimed" and both would write themselves in.
func TestClaimPollerSettlesOneHolderUnderARace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	now := time.Now()
	start := make(chan struct{})
	holders := make(chan string, 2)
	errs := make(chan error, 2)
	for _, claim := range []struct {
		store  *Store
		socket string
	}{{first, "/tmp/first/agentmgr"}, {second, "/tmp/second/agentmgr"}} {
		go func() {
			<-start
			holder, err := claim.store.ClaimPoller(claim.socket, now, time.Second)
			holders <- holder
			errs <- err
		}()
	}
	close(start)

	seen := make([]string, 0, 2)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		seen = append(seen, <-holders)
	}
	if seen[0] != seen[1] {
		t.Fatalf("the two managers came away with different holders: %q and %q", seen[0], seen[1])
	}
	stored, err := first.Setting(PollerSocketKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored != seen[0] {
		t.Fatalf("stored holder = %q, want the one both managers read, %q", stored, seen[0])
	}
}
