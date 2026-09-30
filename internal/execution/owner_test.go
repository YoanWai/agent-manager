package execution

import (
	"errors"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

type recordingInboxOwner struct {
	calls int
	err   error
}

func (owner *recordingInboxOwner) MaintainInbox() error {
	owner.calls++
	return owner.err
}

func TestPollerRoutesInboxMaintenanceAtExistingCadence(t *testing.T) {
	m := buildModel(t)
	owner := &recordingInboxOwner{}
	m.poller.inboxOwner = owner
	for _, tick := range []int{0, 1, inboxPruneEvery} {
		m.poller.tick = tick
		if msg, failed := testPollMessage(m.poller.Step()).(errMsg); failed {
			t.Fatal(msg.err)
		}
	}
	if owner.calls != 2 {
		t.Fatalf("maintenance calls = %d, want 2", owner.calls)
	}
}

func TestPollerOwnerFailureDoesNotPruneLocally(t *testing.T) {
	m := buildModel(t)
	old := time.Now().Add(-48 * time.Hour)
	id, err := m.store.Enqueue(store.InboxMessage{SessionID: "beef", SenderID: "cafe", Body: "expired", Fingerprint: "expired", SentAt: old}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.MarkDelivered(id, old); err != nil {
		t.Fatal(err)
	}
	want := errors.New("owner unavailable")
	m.poller.inboxOwner = &recordingInboxOwner{err: want}
	msg, failed := testPollMessage(m.poller.Step()).(errMsg)
	if !failed || !errors.Is(msg.err, want) {
		t.Fatalf("refresh = %+v", msg)
	}
	if _, err := m.store.Message(id, "cafe"); err != nil {
		t.Fatalf("local fallback pruned message: %v", err)
	}
}
