package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSendModeSurfacesSendFailureAndDoesNotRetry(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("send-tool", "custom", t.TempDir(), "", "cannot deliver", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatal(err)
	}
	sent, err := m.poller.maybeSendPendingInput(sess, "❯ ", true)
	if err == nil || !strings.Contains(err.Error(), "is uncertain") || sent {
		t.Fatalf("send result = %v, %v", sent, err)
	}
	claimed, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.PendingInputClaimed || claimed.PendingInputOutcome != store.DeliveryUncertain {
		t.Fatalf("failed delivery state = %+v", claimed)
	}
	if inputs := sessionPendingInputs(t, m, sess.ID); len(inputs) != 0 {
		t.Fatalf("ambiguous failed delivery was retried: %q", inputs)
	}
}

func TestPendingDeliverySkipsWhileAnotherOwnerHoldsTheGuard(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("send-tool", "custom", t.TempDir(), "", "deliver once", false, false); err != nil {
		t.Fatal(err)
	}
	sess, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := m.store.WithDeliveryGuard(context.Background(), func(guard *store.DeliveryGuard) error {
			if _, claimed, err := guard.ClaimPendingInput(sess.ID, sess.PendingInputs[0], time.Now()); err != nil || !claimed {
				return fmt.Errorf("claim pending input: claimed=%v err=%w", claimed, err)
			}
			close(ready)
			<-release
			return nil
		})
		done <- err
	}()
	<-ready

	if sent, err := m.poller.maybeSendPendingInput(sess, "❯ ", true); err != nil || sent {
		t.Fatalf("peer pending delivery = %v, %v", sent, err)
	}
	state, err := m.store.Get(sess.ID)
	if err != nil || state.PendingInputOutcome != store.DeliveryInFlight || !state.PendingInputClaimed {
		t.Fatalf("peer changed in-flight pending input: %+v err=%v", state, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	state, err = m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sent, err := m.poller.maybeSendPendingInput(state, "", false); err == nil || sent || !strings.Contains(err.Error(), "uncertain prior transport") {
		t.Fatalf("ownerless pending recovery = %v, %v", sent, err)
	}
	state, err = m.store.Get(sess.ID)
	if err != nil || state.PendingInputOutcome != store.DeliveryUncertain || state.PendingInputClaimed || len(state.PendingInputs) != 0 {
		t.Fatalf("pending recovery = %+v err=%v", state, err)
	}
}
