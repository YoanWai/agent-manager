package execution

import (
	"strings"
	"testing"
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
	if err == nil || !strings.Contains(err.Error(), "send pending input") || sent {
		t.Fatalf("send result = %v, %v", sent, err)
	}
	claimed, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed.PendingInputClaimed {
		t.Fatal("failed delivery was not left in an ambiguous durable state")
	}
	sent, err = m.poller.maybeSendPendingInput(claimed, "", false)
	if err == nil || !strings.Contains(err.Error(), "ambiguous pending input") || !sent {
		t.Fatalf("reconcile result = %v, %v", sent, err)
	}
	if inputs := sessionPendingInputs(t, m, sess.ID); len(inputs) != 0 {
		t.Fatalf("ambiguous failed delivery was retried: %q", inputs)
	}
}
