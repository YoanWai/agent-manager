package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTwoManagerStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func backdateHeartbeat(t *testing.T, st *Store, at time.Time) {
	t.Helper()
	if _, err := st.db.Exec(`UPDATE settings SET value = ? WHERE key = ?`,
		fmt.Sprintf("%d", encodeTime(at)), PollerHeartbeatKey); err != nil {
		t.Fatal(err)
	}
}

func TestClaimPollerStealsOnlyStaleStamps(t *testing.T) {
	st := openTwoManagerStore(t)
	socketA := "/tmp/manager-a/tmux-501/agentmgr"
	socketB := "/tmp/manager-b/tmux-501/agentmgr"
	now := time.Now()

	holder, err := st.ClaimPoller(socketA, now, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketA {
		t.Fatalf("first claim = %q, want %q", holder, socketA)
	}

	holder, err = st.ClaimPoller(socketB, now.Add(2*time.Second), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketA {
		t.Fatalf("fresh-stamp claim = %q, want the holder %q to keep it", holder, socketA)
	}

	backdateHeartbeat(t, st, now.Add(-40*time.Second))
	holder, err = st.ClaimPoller(socketB, now.Add(40*time.Second), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketB {
		t.Fatalf("stale-stamp claim = %q, want %q to take over", holder, socketB)
	}
	holder, err = st.ClaimPoller(socketA, now.Add(41*time.Second), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketB {
		t.Fatalf("claim behind a fresh competitor = %q, want %q to stay leading", holder, socketB)
	}

	backdateHeartbeat(t, st, now.Add(35*time.Second))
	holder, err = st.ClaimPoller(socketA, now.Add(80*time.Second), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketA {
		t.Fatalf("reclaim after B's stamp aged = %q, want %q back", holder, socketA)
	}
}

func TestStaleInflightClaimIsRetiredNotRedelivered(t *testing.T) {
	st := openTwoManagerStore(t)
	now := time.Now()
	id, err := st.Enqueue(InboxMessage{
		SessionID: "beefcafe", SenderID: "cafebeef", SenderName: "probe2",
		Body: "matrix ping", Fingerprint: "fp-1", SentAt: now,
	}, DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	claim := claimMessageForTest(t, st, id, now)
	if _, err := st.db.Exec(`UPDATE session_inbox SET claimed_at = ? WHERE id = ?`,
		encodeTime(now.Add(-31*time.Second)), id); err != nil {
		t.Fatal(err)
	}
	dropAt := now.Add(31 * time.Second)
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		return guard.FinishMessage(id, claim, DeliveryRefused, dropAt)
	})

	msg, err := st.Message(id, "cafebeef")
	if err != nil {
		t.Fatal(err)
	}
	if !msg.DeliveredAt.Equal(dropAt) {
		t.Fatalf("retired message delivered_at = %v, want %v", msg.DeliveredAt, dropAt)
	}
	if msg.DroppedAt.IsZero() {
		t.Fatal("retired message carries no drop mark, so the sender reads it as delivered")
	}
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		_, again, err := guard.ClaimMessage(id, now.Add(32*time.Second))
		if err != nil || again {
			t.Fatalf("claim after drop = %v, %v; want refused", again, err)
		}
		return nil
	})
	before := msg
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		err := guard.FinishMessage(id, claim, DeliveryConfirmed, now.Add(33*time.Second))
		if !errors.Is(err, ErrDeliveryReceiptRejected) {
			t.Fatalf("late receipt = %v, want ErrDeliveryReceiptRejected", err)
		}
		return nil
	})
	msg, err = st.Message(id, "cafebeef")
	if err != nil {
		t.Fatal(err)
	}
	if msg.DeliveredAt != before.DeliveredAt || msg.DroppedAt != before.DroppedAt {
		t.Fatalf("late delivery rewrote the retired row: %+v then %+v", before, msg)
	}
}

func TestClaimMessageIsSingleWinnerUnderConcurrentClaims(t *testing.T) {
	st := openTwoManagerStore(t)
	var seq int
	var name, path string
	if err := st.db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	claimers := []*Store{st, other}
	now := time.Now()
	id, err := st.Enqueue(InboxMessage{
		SessionID: "beefcafe", SenderID: "cafebeef", SenderName: "probe2",
		Body: "matrix ping", Fingerprint: "fp-2", SentAt: now,
	}, DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	wins := make([]bool, workers)
	errors := make([]error, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var claimed bool
			acquired, err := claimers[i%len(claimers)].WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
				_, claimed, err = guard.ClaimMessage(id, now)
				return err
			})
			if err == nil && !acquired {
				claimed = false
			}
			mu.Lock()
			wins[i] = claimed
			errors[i] = err
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	total := 0
	for i, w := range wins {
		if errors[i] != nil {
			t.Fatal(errors[i])
		}
		if w {
			total++
		}
	}
	if total != 1 {
		t.Fatalf("concurrent claims = %d winners, want exactly one", total)
	}
}

func TestManagerAwakeAgesWithTheNewestStamp(t *testing.T) {
	st := openTwoManagerStore(t)
	now := time.Now()
	if _, err := st.ClaimPoller("/tmp/a/agentmgr", now, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	awake, err := st.ManagerAwake(now.Add(1*time.Second), 2*time.Second)
	if err != nil || !awake {
		t.Fatalf("fresh stamp awake = %v, %v", awake, err)
	}
	backdateHeartbeat(t, st, now.Add(-31*time.Second))
	awake, err = st.ManagerAwake(now.Add(31*time.Second), 2*time.Second)
	if err != nil || awake {
		t.Fatalf("aged stamp awake = %v, %v; want the manager reading as closed", awake, err)
	}
	if _, err := st.ClaimPoller("/tmp/b/agentmgr", now.Add(40*time.Second), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	awake, err = st.ManagerAwake(now.Add(41*time.Second), 2*time.Second)
	if err != nil || !awake {
		t.Fatalf("competitor stamp awake = %v, %v; want the profile to stay awake", awake, err)
	}
}
