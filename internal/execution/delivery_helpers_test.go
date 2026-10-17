package execution

import (
	"context"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

func withStoreDeliveryGuard(t *testing.T, st *store.Store, fn func(*store.DeliveryGuard) error) {
	t.Helper()
	acquired, err := st.WithDeliveryGuard(context.Background(), fn)
	if err != nil || !acquired {
		t.Fatalf("delivery guard: acquired=%v err=%v", acquired, err)
	}
}

func claimInboxForTest(t *testing.T, st *store.Store, id int64, at time.Time) store.DeliveryClaim {
	t.Helper()
	var claim store.DeliveryClaim
	withStoreDeliveryGuard(t, st, func(guard *store.DeliveryGuard) error {
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

func finishInboxForTest(t *testing.T, st *store.Store, id int64, outcome store.DeliveryOutcome, at time.Time) {
	t.Helper()
	withStoreDeliveryGuard(t, st, func(guard *store.DeliveryGuard) error {
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

func finishPendingForTest(t *testing.T, st *store.Store, id, input string, at time.Time) {
	t.Helper()
	withStoreDeliveryGuard(t, st, func(guard *store.DeliveryGuard) error {
		claim, claimed, err := guard.ClaimPendingInput(id, input, at)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatalf("pending input %q was not claimed", input)
		}
		return guard.FinishPendingInput(id, input, claim, store.DeliveryConfirmed, at)
	})
}
