package store

import (
	"path/filepath"
	"slices"
	"testing"
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
	claimed, err := st.ClaimPendingInput("a", "second")
	if err != nil || claimed {
		t.Fatalf("claim out of order = %v, %v", claimed, err)
	}
	claimed, err = st.ClaimPendingInput("a", "first")
	if err != nil || !claimed {
		t.Fatalf("claim first = %v, %v", claimed, err)
	}
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
	consumed, err := st.ConsumeClaimedPendingInput("a", "first")
	if err != nil || !consumed {
		t.Fatalf("consume first = %v, %v", consumed, err)
	}
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
