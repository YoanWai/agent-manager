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

func TestLegacyDeliveryWritesAreRejectedByTheDatabase(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	id, err := st.Enqueue(message("legacy writer", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE session_inbox SET claimed_at = ? WHERE id = ?`, encodeTime(now), id); err == nil {
		t.Fatal("legacy inbox claim bypassed the delivery fence")
	}

	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"one"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE sessions SET pending_claimed = 1 WHERE id = ?`, "pending"); err == nil {
		t.Fatal("legacy pending-input claim bypassed the delivery fence")
	}

	var claim DeliveryClaim
	acquired, err := st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		var claimed bool
		claim, claimed, err = guard.ClaimMessage(id, now)
		if err == nil && !claimed {
			err = errors.New("guard did not claim inbox row")
		}
		return err
	})
	if err != nil || !acquired {
		t.Fatalf("guard claim: acquired=%v err=%v", acquired, err)
	}
	if claim.AttemptToken == "" {
		t.Fatal("claim has no attempt token")
	}
	if _, err := st.db.Exec(`UPDATE session_inbox SET delivered_at = ? WHERE id = ?`, encodeTime(now), id); err == nil {
		t.Fatal("legacy inbox receipt bypassed the delivery fence")
	}
}

func TestInboxClaimSurvivesRestartAndIsRecoveredAsUncertain(t *testing.T) {
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
		if msg.Outcome != DeliveryInFlight {
			return errors.New("durable claim lost its ownership metadata")
		}
		return guard.RecoverMessage(msg.ID, time.Now())
	})
	if err != nil || !acquired {
		t.Fatalf("recover: acquired=%v err=%v", acquired, err)
	}
	state, err := st.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != DeliveryUncertain || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() {
		t.Fatalf("recovered state = %+v", state)
	}
	if _, ok, err := st.HeadMessage("target01"); err != nil || ok {
		t.Fatalf("uncertain message remained replayable: ok=%v err=%v", ok, err)
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
}

func TestPendingClaimRecoveryConsumesOnlyTheUncertainHead(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{ID: "pending", Name: "pending", Tool: "claude", Cwd: "/tmp", PendingInputs: []string{"first", "second"}}); err != nil {
		t.Fatal(err)
	}
	acquired, err := st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		_, claimed, err := guard.ClaimPendingInput("pending", "first", time.Now())
		if err == nil && !claimed {
			err = errors.New("pending claim lost")
		}
		return err
	})
	if err != nil || !acquired {
		t.Fatalf("claim: acquired=%v err=%v", acquired, err)
	}
	acquired, err = st.WithDeliveryGuard(context.Background(), func(guard *DeliveryGuard) error {
		return guard.RecoverPendingInput("pending", "first", time.Now())
	})
	if err != nil || !acquired {
		t.Fatalf("recover: acquired=%v err=%v", acquired, err)
	}
	sess, err := st.Get("pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.PendingInputs) != 1 || sess.PendingInputs[0] != "second" || sess.PendingInputClaimed {
		t.Fatalf("pending recovery changed the wrong inputs: %+v", sess)
	}
	if sess.PendingInputOutcome != DeliveryUncertain {
		t.Fatalf("pending recovery has no uncertain receipt: %+v", sess)
	}
}

func TestMigrationRetiresAnAlreadyAdmittedLegacyClaimAsUncertain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	id, err := st.Enqueue(message("old in-flight send", now), DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	// Stand in for the stopped old binary: its schema has no fences and its
	// claim has no token. The cutover migration must never call this refused.
	if _, err := st.db.Exec(`
DROP TRIGGER session_inbox_delivery_claim_fence;
DROP TRIGGER session_inbox_delivery_receipt_fence;
UPDATE session_inbox SET claimed_at = ? WHERE id = ?`, encodeTime(now), id); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	state, err := st.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != DeliveryUncertain || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() {
		t.Fatalf("migrated legacy claim = %+v", state)
	}
	if _, ok, err := st.HeadMessage("target01"); err != nil || ok {
		t.Fatalf("legacy claim remained replayable: ok=%v err=%v", ok, err)
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
