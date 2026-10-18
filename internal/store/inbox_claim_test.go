package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
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

	holder, err := pollerHolder(st, socketA, now)
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketA {
		t.Fatalf("first claim = %q, want %q", holder, socketA)
	}

	holder, err = pollerHolder(st, socketB, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketA {
		t.Fatalf("fresh-stamp claim = %q, want the holder %q to keep it", holder, socketA)
	}

	backdateHeartbeat(t, st, now.Add(-40*time.Second))
	holder, err = pollerHolder(st, socketB, now.Add(40*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketB {
		t.Fatalf("stale-stamp claim = %q, want %q to take over", holder, socketB)
	}
	holder, err = pollerHolder(st, socketA, now.Add(41*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if holder != socketB {
		t.Fatalf("claim behind a fresh competitor = %q, want %q to stay leading", holder, socketB)
	}

	backdateHeartbeat(t, st, now.Add(35*time.Second))
	holder, err = pollerHolder(st, socketA, now.Add(80*time.Second))
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
	if _, err := st.ClaimPoller("/tmp/a/agentmgr", now); err != nil {
		t.Fatal(err)
	}
	awake, err := st.ManagerAwake(now.Add(1 * time.Second))
	if err != nil || !awake {
		t.Fatalf("fresh stamp awake = %v, %v", awake, err)
	}
	backdateHeartbeat(t, st, now.Add(-31*time.Second))
	awake, err = st.ManagerAwake(now.Add(31 * time.Second))
	if err != nil || awake {
		t.Fatalf("aged stamp awake = %v, %v; want the manager reading as closed", awake, err)
	}
	if _, err := st.ClaimPoller("/tmp/b/agentmgr", now.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	awake, err = st.ManagerAwake(now.Add(41 * time.Second))
	if err != nil || !awake {
		t.Fatalf("competitor stamp awake = %v, %v; want the profile to stay awake", awake, err)
	}
}

// olderClaimPoller is the stamp a manager older than the delivery guard
// writes, verbatim from v0.42.0 internal/store/inbox.go: the socket and
// the heartbeat, and nothing beside them.
func olderClaimPoller(t *testing.T, st *Store, socket string, now time.Time) {
	t.Helper()
	if _, err := st.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?), (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		PollerSocketKey, socket,
		PollerHeartbeatKey, strconv.FormatInt(now.UnixNano(), 10)); err != nil {
		t.Fatal(err)
	}
}

func TestClaimPollerLeavesDeliveryToAnOlderManagerUntilItStops(t *testing.T) {
	st := openTwoManagerStore(t)
	socket := "/tmp/manager-a/tmux-501/agentmgr"
	now := time.Now()

	olderClaimPoller(t, st, socket, now)
	claim, err := st.ClaimPoller(socket, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claim.Holder != socket || !claim.OlderManager {
		t.Fatalf("claim beside a fresh older stamp = %+v, want delivery left to it", claim)
	}
	awake, err := st.ManagerAwake(now.Add(3 * time.Second))
	if err != nil || !awake {
		t.Fatalf("awake = %v, %v; the shared heartbeat must keep reading as home", awake, err)
	}

	// The older manager restamps between this manager's claims; each of
	// its stamps is overwritten by the next guarded claim, which is the
	// one read that must notice it.
	olderClaimPoller(t, st, socket, now.Add(8*time.Second))
	claim, err = st.ClaimPoller(socket, now.Add(12*time.Second))
	if err != nil || !claim.OlderManager {
		t.Fatalf("claim after the older restamp = %+v, %v", claim, err)
	}
	claim, err = st.ClaimPoller(socket, now.Add(22*time.Second))
	if err != nil || !claim.OlderManager {
		t.Fatalf("claim with only this manager's own stamps since = %+v, %v; the older one is within its window", claim, err)
	}

	claim, err = st.ClaimPoller(socket, now.Add(8*time.Second+PollerHeartbeatStale))
	if err != nil || claim.OlderManager {
		t.Fatalf("claim once the older stamp aged = %+v, %v", claim, err)
	}
	claim, err = st.ClaimPoller(socket, now.Add(60*time.Second))
	if err != nil || claim.OlderManager || claim.Holder != socket {
		t.Fatalf("claim with no older manager = %+v, %v", claim, err)
	}
}

func TestClaimPollerSharesTheOlderStampBetweenGuardedManagers(t *testing.T) {
	st := openTwoManagerStore(t)
	socket := "/tmp/manager-a/tmux-501/agentmgr"
	now := time.Now()
	if _, err := st.ClaimPoller(socket, now); err != nil {
		t.Fatal(err)
	}
	olderClaimPoller(t, st, socket, now.Add(3*time.Second))
	// A second guarded manager on the socket overwrites the older stamp.
	claim, err := st.ClaimPoller(socket, now.Add(5*time.Second))
	if err != nil || !claim.OlderManager {
		t.Fatalf("second manager's claim = %+v, %v", claim, err)
	}
	// The first reads its own peer's stamp in the heartbeat and still
	// learns of the older manager from the row the peer left.
	claim, err = st.ClaimPoller(socket, now.Add(10*time.Second))
	if err != nil || !claim.OlderManager {
		t.Fatalf("first manager's claim behind its peer = %+v, %v", claim, err)
	}
}

// An older manager on another tmux server types into panes this one
// cannot see, so its stamp holds the store but not delivery here.
func TestClaimPollerIgnoresAnOlderManagerOnAnotherServer(t *testing.T) {
	st := openTwoManagerStore(t)
	socketA := "/tmp/manager-a/tmux-501/agentmgr"
	socketB := "/tmp/manager-b/tmux-501/agentmgr"
	now := time.Now()
	olderClaimPoller(t, st, socketA, now)
	claim, err := st.ClaimPoller(socketB, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claim.Holder != socketA || claim.OlderManager {
		t.Fatalf("claim from another server = %+v, want the older holder kept and delivery free", claim)
	}
}
