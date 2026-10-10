package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func withDeliveryGuard(t *testing.T, st *Store, fn func(*DeliveryGuard) error) {
	t.Helper()
	acquired, err := st.WithDeliveryGuard(context.Background(), fn)
	if err != nil || !acquired {
		t.Fatalf("delivery guard: acquired=%v err=%v", acquired, err)
	}
}

func claimMessageForTest(t *testing.T, st *Store, id int64, at time.Time) DeliveryClaim {
	t.Helper()
	var claim DeliveryClaim
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		var claimed bool
		var err error
		claim, claimed, err = guard.ClaimMessage(id, at)
		if err == nil && !claimed {
			t.Fatalf("message %d was not claimed", id)
		}
		return err
	})
	return claim
}

func finishMessageForTest(t *testing.T, st *Store, id int64, outcome DeliveryOutcome, at time.Time) {
	t.Helper()
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		claim, claimed, err := guard.ClaimMessage(id, at)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatalf("message %d was not claimed", id)
		}
		return guard.FinishMessage(id, claim, outcome, at)
	})
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sample(id, group string) Session {
	return Session{ID: id, Name: "n-" + id, Tool: "claude", Cwd: "/tmp", Group: group, Status: "idle"}
}
