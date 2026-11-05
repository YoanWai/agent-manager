package execution

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestAckedWaitingHoldsIdleUntilTheScreenChanges(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "ackwait01", Tool: "claude-hooked"}
	pane := "Run the tests?\nEnter to confirm\n❯ \n"
	writeHookStatus(t, m, sess.ID, status.Finished)

	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("the prompt should derive waiting, got %q", got)
	}
	sess.Status, sess.Acked = status.Idle, true
	for range 2 {
		if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
			t.Fatalf("an acked waiting screen should hold idle, got %q", got)
		}
	}
	next := "Run the tests?\nyes\nDelete the cache?\nEnter to confirm\n❯ \n"
	if got := deriveStatus(t, m, sess, next, true); got != status.Waiting {
		t.Fatalf("a new prompt should derive waiting again, got %q", got)
	}
}

func TestAckOfAFinishedTurnDoesNotHoldALaterPrompt(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "ackwait02", Tool: "claude-hooked", Status: status.Idle, Acked: true}
	writeHookStatus(t, m, sess.ID, status.Finished)
	if got := deriveStatus(t, m, sess, "Run the tests?\nEnter to confirm\n❯ \n", true); got != status.Waiting {
		t.Fatalf("a prompt no ack saw should derive waiting, got %q", got)
	}
}
