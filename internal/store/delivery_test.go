package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const deliveryGuardHelperEnv = "AGENT_MANAGER_DELIVERY_GUARD_HELPER"

func TestDeliveryGuardHelperProcess(t *testing.T) {
	dbPath := os.Getenv(deliveryGuardHelperEnv)
	if dbPath == "" {
		return
	}
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ready := os.Getenv("AGENT_MANAGER_DELIVERY_GUARD_READY")
	acquired, err := st.WithDeliveryGuard(context.Background(), func(*DeliveryGuard) error {
		if err := os.WriteFile(ready, nil, 0o600); err != nil {
			return err
		}
		_, _ = os.Stdin.Read(make([]byte, 1))
		return nil
	})
	if err != nil || !acquired {
		t.Fatalf("helper guard: acquired=%v err=%v", acquired, err)
	}
}

func TestDeliveryGuardExcludesAnotherProcessWithoutWaiting(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ready := filepath.Join(dir, "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestDeliveryGuardHelperProcess$")
	cmd.Env = append(os.Environ(), deliveryGuardHelperEnv+"="+dbPath, "AGENT_MANAGER_DELIVERY_GUARD_READY="+ready)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper never acquired delivery guard")
		}
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	acquired, err := st.WithDeliveryGuard(context.Background(), func(*DeliveryGuard) error {
		t.Fatal("guard callback ran while another process held the profile")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("second process acquired the delivery guard")
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("nonblocking guard took %v", elapsed)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

// The statements a manager older than the guard runs, verbatim from its
// store (v0.42.0 internal/store/inbox.go and store.go). They know nothing
// of the receipt columns and must keep succeeding on a profile a newer
// binary has opened, since that manager keeps delivering beside it.
// Claims run once per PollerHeartbeatPeriod; a delivery reads the
// heartbeat again under the guard so an older manager that stamped since
// is seen before anything is claimed.
func TestDeliveryGuardSeesAnOlderStampBetweenClaims(t *testing.T) {
	st := openTwoManagerStore(t)
	socket := "/tmp/manager-a/tmux-501/agentmgr"
	other := "/tmp/manager-b/tmux-501/agentmgr"
	now := time.Now()
	if _, err := st.ClaimPoller(socket, now); err != nil {
		t.Fatal(err)
	}
	stamped := func(at time.Time) bool {
		t.Helper()
		var seen bool
		acquired, err := st.WithDeliveryGuard(context.Background(), func(g *DeliveryGuard) error {
			var err error
			seen, err = g.OlderManagerStamped(socket, at)
			return err
		})
		if err != nil || !acquired {
			t.Fatalf("guard: acquired=%v err=%v", acquired, err)
		}
		return seen
	}
	if stamped(now) {
		t.Fatal("own stamp read as an older manager's")
	}
	olderStamp := now.Add(3 * time.Second)
	olderClaimPoller(t, st, socket, olderStamp)
	if !stamped(olderStamp) {
		t.Fatal("older stamp between claims not seen under the guard")
	}
	// The claim writes over the older stamp. A second guarded manager,
	// whose own claim has not run yet, still sees the one it recorded.
	if _, err := st.ClaimPoller(socket, now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !stamped(now.Add(10 * time.Second)) {
		t.Fatal("recorded older stamp not seen once a claim wrote over it")
	}
	aged := olderStamp.Add(PollerHeartbeatStale + time.Second)
	if stamped(aged) {
		t.Fatal("aged older stamp read as an older manager still delivering")
	}
	olderClaimPoller(t, st, other, aged)
	if stamped(aged) {
		t.Fatal("older manager on another server read as delivering here")
	}
}

func olderClaimMessage(st *Store, id int64, at time.Time) (bool, error) {
	res, err := st.db.Exec(
		`UPDATE session_inbox SET claimed_at = ? WHERE id = ? AND claimed_at = 0 AND delivered_at = 0`,
		encodeTime(at), id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}

func olderMarkDelivered(st *Store, id int64, at time.Time) error {
	_, err := st.db.Exec(
		`UPDATE session_inbox SET delivered_at = ? WHERE id = ? AND delivered_at = 0`,
		encodeTime(at), id)
	return err
}

func olderMarkDropped(st *Store, id int64, at time.Time) error {
	stamp := encodeTime(at)
	_, err := st.db.Exec(
		`UPDATE session_inbox SET delivered_at = ?, dropped_at = ? WHERE id = ? AND delivered_at = 0`,
		stamp, stamp, id)
	return err
}

func olderClaimPendingInput(st *Store, id, encoded string) (bool, error) {
	res, err := st.db.Exec(
		`UPDATE sessions SET pending_claimed = 1
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 0`, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}

func olderConsumeClaimedPendingInput(st *Store, id, remaining, encoded string) (bool, error) {
	res, err := st.db.Exec(
		`UPDATE sessions SET pending_inputs = ?, pending_claimed = 0
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1`,
		remaining, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}

func TestOlderManagerDeliveryWritesSucceedOnTheNewSchema(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	delivered, err := st.Enqueue(message("older delivered", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := olderClaimMessage(st, delivered, now); err != nil || !claimed {
		t.Fatalf("older claim = %v, %v", claimed, err)
	}
	if err := olderMarkDelivered(st, delivered, now.Add(time.Second)); err != nil {
		t.Fatalf("older delivered mark: %v", err)
	}
	dropped, err := st.Enqueue(message("older dropped", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := olderClaimMessage(st, dropped, now); err != nil || !claimed {
		t.Fatalf("older claim = %v, %v", claimed, err)
	}
	if err := olderMarkDropped(st, dropped, now.Add(time.Second)); err != nil {
		t.Fatalf("older drop mark: %v", err)
	}

	state, err := st.Message(delivered, "sender01")
	if err != nil || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() || state.AttemptToken != "" || state.Outcome != "" {
		t.Fatalf("older delivery reads back as %+v, %v", state, err)
	}
	state, err = st.Message(dropped, "sender01")
	if err != nil || state.DroppedAt.IsZero() {
		t.Fatalf("older drop reads back as %+v, %v", state, err)
	}
	if err := st.MarkRead("target01", "sender01", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if state, err := st.Message(delivered, "sender01"); err != nil || state.ReadAt.IsZero() {
		t.Fatalf("a reply did not ack the older manager's delivery: %+v, %v", state, err)
	}
	if _, ok, err := st.HeadMessage("target01"); err != nil || ok {
		t.Fatalf("older resolutions left the queue populated: ok=%v err=%v", ok, err)
	}

	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"one", "two"}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := olderClaimPendingInput(st, "pending", `["one","two"]`); err != nil || !claimed {
		t.Fatalf("older pending claim = %v, %v", claimed, err)
	}
	sess, err := st.Get("pending")
	if err != nil || !sess.PendingInputClaimed {
		t.Fatalf("older pending claim unread: %+v, %v", sess, err)
	}
	if consumed, err := olderConsumeClaimedPendingInput(st, "pending", `["two"]`, `["one","two"]`); err != nil || !consumed {
		t.Fatalf("older pending consume = %v, %v", consumed, err)
	}
	sess, err = st.Get("pending")
	if err != nil || sess.PendingInputClaimed || len(sess.PendingInputs) != 1 || sess.PendingInputs[0] != "two" {
		t.Fatalf("older pending consume unread: %+v, %v", sess, err)
	}
}

// A main build between e3298ff and 3f38326 installed triggers that refuse
// the older manager's claim. A profile that build opened keeps them until
// something drops them.
func TestOpenDropsTheFenceTriggersAnInterimBuildInstalled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`
CREATE TRIGGER session_inbox_delivery_claim_fence
 BEFORE UPDATE OF claimed_at ON session_inbox
 WHEN OLD.claimed_at = 0 AND NEW.claimed_at != 0 AND NEW.attempt_token = ''
 BEGIN SELECT RAISE(ABORT, 'delivery claim requires an attempt token'); END;
CREATE TRIGGER session_pending_delivery_claim_fence
 BEFORE UPDATE OF pending_claimed ON sessions
 WHEN OLD.pending_claimed = 0 AND NEW.pending_claimed != 0 AND NEW.pending_attempt_token = ''
 BEGIN SELECT RAISE(ABORT, 'pending delivery claim requires an attempt token'); END`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id, err := st.Enqueue(message("fenced", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := olderClaimMessage(st, id, now); err == nil {
		t.Fatal("fixture triggers do not refuse the older claim")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if claimed, err := olderClaimMessage(st, id, now); err != nil || !claimed {
		t.Fatalf("older claim after reopen = %v, %v", claimed, err)
	}
	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"one"}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := olderClaimPendingInput(st, "pending", `["one"]`); err != nil || !claimed {
		t.Fatalf("older pending claim after reopen = %v, %v", claimed, err)
	}
}

func TestInboxClaimSurvivesRestartAndIsRetiredAsUncertain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id, err := st.Enqueue(message("crash boundary", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		_, claimed, err := guard.ClaimMessage(id, now)
		if err == nil && !claimed {
			err = errors.New("claim lost")
		}
		return err
	})
	if err != nil || !acquired {
		t.Fatalf("claim: acquired=%v err=%v", acquired, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acquired, err = st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		msg, ok, err := guard.HeadMessage("target01")
		if err != nil || !ok {
			return errors.New("durable claim disappeared")
		}
		if msg.Outcome != DeliveryInFlight || msg.AttemptToken == "" {
			return errors.New("durable claim lost its ownership metadata")
		}
		return guard.RetireMessage(msg.ID, time.Now())
	})
	if err != nil || !acquired {
		t.Fatalf("retire: acquired=%v err=%v", acquired, err)
	}
	state, err := st.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	// delivered_at stamped with no drop mark is what an older reader calls
	// delivered: the one answer that never provokes a resend.
	if state.Outcome != DeliveryUncertain || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() {
		t.Fatalf("retired state = %+v", state)
	}
	if _, ok, err := st.HeadMessage("target01"); err != nil || ok {
		t.Fatalf("uncertain message remained replayable: ok=%v err=%v", ok, err)
	}
}

func TestOlderManagerClaimsAreRetiredUncertainNeverReclaimed(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	id, err := st.Enqueue(message("older in-flight send", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := olderClaimMessage(st, id, now); err != nil || !claimed {
		t.Fatalf("older claim = %v, %v", claimed, err)
	}
	head, ok, err := st.HeadMessage("target01")
	if err != nil || !ok || head.ClaimedAt.IsZero() || head.AttemptToken != "" {
		t.Fatalf("older claim reads as %+v, ok=%v, %v", head, ok, err)
	}
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		if _, again, err := guard.ClaimMessage(id, now.Add(time.Second)); err != nil || again {
			t.Fatalf("guarded claim over the older one = %v, %v", again, err)
		}
		return guard.RetireMessage(id, now.Add(31*time.Second))
	})
	state, err := st.Message(id, "sender01")
	if err != nil || state.Outcome != DeliveryUncertain || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() {
		t.Fatalf("retired older claim = %+v, %v", state, err)
	}
	// The older manager finishing late finds the row resolved and leaves it.
	if err := olderMarkDelivered(st, id, now.Add(32*time.Second)); err != nil {
		t.Fatal(err)
	}
	if after, err := st.Message(id, "sender01"); err != nil || after.DeliveredAt != state.DeliveredAt {
		t.Fatalf("late older receipt rewrote the retired row: %+v then %+v", state, after)
	}

	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"first", "second"}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := olderClaimPendingInput(st, "pending", `["first","second"]`); err != nil || !claimed {
		t.Fatalf("older pending claim = %v, %v", claimed, err)
	}
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		return guard.RetirePendingInput("pending", "first")
	})
	sess, err := st.Get("pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.PendingInputs) != 1 || sess.PendingInputs[0] != "second" || sess.PendingInputClaimed || sess.PendingInputOutcome != DeliveryUncertain {
		t.Fatalf("retired older pending claim = %+v", sess)
	}
}

func TestReceiptRequiresTheMatchingClaimToken(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	id, err := st.Enqueue(message("token fence", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		claim, claimed, err := guard.ClaimMessage(id, now)
		if err != nil || !claimed {
			return errors.New("claim failed")
		}
		claim.AttemptToken += "-wrong"
		if err := guard.FinishMessage(id, claim, DeliveryConfirmed, now.Add(time.Second)); !errors.Is(err, ErrDeliveryReceiptRejected) {
			return errors.New("wrong token was not rejected")
		}
		return guard.FinishMessage(id, DeliveryClaim{AttemptToken: claim.AttemptToken[:len(claim.AttemptToken)-6]}, DeliveryConfirmed, now.Add(time.Second))
	})
	if err != nil || !acquired {
		t.Fatalf("finish: acquired=%v err=%v", acquired, err)
	}
	state, err := st.Message(id, "sender01")
	if err != nil || state.Outcome != DeliveryConfirmed || state.DeliveredAt.IsZero() {
		t.Fatalf("confirmed receipt = %+v, %v", state, err)
	}
}

// An older manager that finds a guarded claim past its grace drops it the
// way it drops its own. The receipt then finds the row resolved and is
// refused rather than overwriting what that manager told the sender.
func TestReceiptIsRefusedOnceAnOlderManagerRetiredTheClaim(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	id, err := st.Enqueue(message("stalled paste", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	claim := claimMessageForTest(t, st, id, now)
	if err := olderMarkDropped(st, id, now.Add(31*time.Second)); err != nil {
		t.Fatal(err)
	}
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		if err := guard.FinishMessage(id, claim, DeliveryConfirmed, now.Add(32*time.Second)); !errors.Is(err, ErrDeliveryReceiptRejected) {
			t.Fatalf("late receipt = %v, want ErrDeliveryReceiptRejected", err)
		}
		return nil
	})
	state, err := st.Message(id, "sender01")
	if err != nil || state.Outcome != DeliveryInFlight || state.DroppedAt.IsZero() {
		t.Fatalf("row after the older drop = %+v, %v", state, err)
	}
}

// An older manager claims pending input with no token and consumes a claim
// it finds without a receipt, so a reader shows no outcome for its claim
// and reads a guarded attempt it consumed as uncertain, never as in flight.
func TestPendingOutcomeReadsAnOlderManagersClaimsCoherently(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"first", "second"}}); err != nil {
		t.Fatal(err)
	}
	outcome := func(want DeliveryOutcome, when string) {
		t.Helper()
		sess, err := st.Get("pending")
		if err != nil || sess.PendingInputOutcome != want {
			t.Fatalf("outcome %s = %q, %v; want %q", when, sess.PendingInputOutcome, err, want)
		}
	}
	withDeliveryGuard(t, st, func(guard *DeliveryGuard) error {
		_, claimed, err := guard.ClaimPendingInput("pending", "first")
		if err == nil && !claimed {
			err = errors.New("pending claim lost")
		}
		return err
	})
	outcome(DeliveryInFlight, "under a guarded claim")
	if ok, err := olderConsumeClaimedPendingInput(st, "pending", `["second"]`, `["first","second"]`); err != nil || !ok {
		t.Fatalf("older consume = %v, %v", ok, err)
	}
	outcome(DeliveryUncertain, "after an older manager consumed the guarded claim")
	if ok, err := olderClaimPendingInput(st, "pending", `["second"]`); err != nil || !ok {
		t.Fatalf("older claim = %v, %v", ok, err)
	}
	outcome("", "under an older manager's claim")
	if ok, err := olderConsumeClaimedPendingInput(st, "pending", `[]`, `["second"]`); err != nil || !ok {
		t.Fatalf("older consume = %v, %v", ok, err)
	}
	outcome(DeliveryUncertain, "after an older manager consumed its own claim")
}

func TestPendingClaimRetirementConsumesOnlyTheUncertainHead(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"first", "second"}}); err != nil {
		t.Fatal(err)
	}
	acquired, err := st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		_, claimed, err := guard.ClaimPendingInput("pending", "first")
		if err == nil && !claimed {
			err = errors.New("pending claim lost")
		}
		return err
	})
	if err != nil || !acquired {
		t.Fatalf("claim: acquired=%v err=%v", acquired, err)
	}
	acquired, err = st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		return guard.RetirePendingInput("pending", "first")
	})
	if err != nil || !acquired {
		t.Fatalf("retire: acquired=%v err=%v", acquired, err)
	}
	sess, err := st.Get("pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.PendingInputs) != 1 || sess.PendingInputs[0] != "second" || sess.PendingInputClaimed {
		t.Fatalf("pending retirement changed the wrong inputs: %+v", sess)
	}
	if sess.PendingInputOutcome != DeliveryUncertain {
		t.Fatalf("pending retirement has no uncertain receipt: %+v", sess)
	}
}

func TestDeliveryGuardUsesOneIdentityThroughDatabaseSymlink(t *testing.T) {
	dir := t.TempDir()
	primary, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	alias := filepath.Join(dir, "alias.db")
	if err := os.Symlink(primary.path, alias); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	acquired, err := primary.WithDeliveryGuard(context.Background(), func(*DeliveryGuard) error {
		peerAcquired, peerErr := peer.WithDeliveryGuard(context.Background(), func(*DeliveryGuard) error { return nil })
		if peerErr != nil {
			t.Fatal(peerErr)
		}
		if peerAcquired {
			t.Fatal("database alias admitted a second owner")
		}
		return nil
	})
	if err != nil || !acquired {
		t.Fatalf("guard acquired=%v err=%v", acquired, err)
	}
}
