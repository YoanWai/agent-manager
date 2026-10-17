package store

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestPendingInputsPersistAndConsumeInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sess := sample("a", "g1")
	sess.PendingInputs = []string{"first", "second"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !slices.Equal(got.PendingInputs, []string{"first", "second"}) {
		t.Fatalf("pending inputs = %q", got.PendingInputs)
	}
	var claim DeliveryClaim
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		_, claimed, err := guard.ClaimPendingInput("a", "second", time.Now())
		if err != nil || claimed {
			t.Fatalf("claim out of order = %v, %v", claimed, err)
		}
		var ok bool
		claim, ok, err = guard.ClaimPendingInput("a", "first", time.Now())
		if err != nil || !ok {
			t.Fatalf("claim first = %v, %v", ok, err)
		}
		return nil
	})
	if err := st.Close(); err != nil {
		t.Fatalf("close claimed store: %v", err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen claimed store: %v", err)
	}
	defer st.Close()
	got, err = st.Get("a")
	if err != nil {
		t.Fatalf("get claimed: %v", err)
	}
	if !got.PendingInputClaimed {
		t.Fatal("pending delivery claim did not survive reopen")
	}
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		return guard.FinishPendingInput("a", "first", claim, DeliveryConfirmed, time.Now())
	})
	got, err = st.Get("a")
	if err != nil {
		t.Fatalf("get after consume: %v", err)
	}
	if !slices.Equal(got.PendingInputs, []string{"second"}) {
		t.Fatalf("remaining inputs = %q", got.PendingInputs)
	}
	if got.PendingInputClaimed {
		t.Fatal("delivery claim remained after consumption")
	}
}
