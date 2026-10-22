package sessioncmd

import (
	"strings"
	"testing"
)

// The user's quick bar on another machine types into the pane at once, as
// the local one does: nothing is queued, and a repeat goes in again.
func TestTypeSubmitsThePromptInThePaneAtOnce(t *testing.T) {
	h := newSessionHarness(t)
	if err := h.store.SetAcked(h.caller.ID, true); err != nil {
		t.Fatalf("SetAcked: %v", err)
	}
	typed, err := h.sessions.Type("", h.caller.ID, "echo typed-$((40+2))")
	if err != nil {
		t.Fatalf("Type: %v", err)
	}
	if typed.ID != h.caller.ID || !typed.Running {
		t.Fatalf("typed row = %+v", typed)
	}
	waitForSessionOutput(t, h.sessions, "", h.caller.ID, "typed-42")
	if queued, err := h.store.QueuedCount(h.caller.ID); err != nil || queued != 0 {
		t.Fatalf("Type queued %d messages (%v)", queued, err)
	}
	row, err := h.store.Get(h.caller.ID)
	if err != nil || row.Acked || row.LastPrompt != "echo typed-$((40+2))" {
		t.Fatalf("row after Type = acked %v prompt %q (%v)", row.Acked, row.LastPrompt, err)
	}
	if _, err := h.sessions.Type("", h.caller.ID, "echo typed-$((40+2))"); err != nil {
		t.Fatalf("the same prompt again: %v", err)
	}
}

func TestTypeRefusesACallingSessionAndAnUnreachableTarget(t *testing.T) {
	h := newSessionHarness(t)
	worker := h.addSessionRow(t, "worker")
	if _, err := h.sessions.Type(worker, h.caller.ID, "hi"); err == nil || !strings.Contains(err.Error(), "send_session") {
		t.Fatalf("Type from a session = %v, want it pointed at send", err)
	}
	if _, err := h.sessions.Type("", h.caller.ID, "  "); err == nil || err.Error() != "prompt cannot be empty" {
		t.Fatalf("an empty prompt = %v", err)
	}
	if _, err := h.sessions.Type("", worker, "hi"); err == nil || !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("a session with no pane = %v", err)
	}
	terminal, err := h.terminals.Create("", CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create terminal: %v", err)
	}
	if _, err := h.sessions.Type("", terminal.ID, "ls"); err == nil || !strings.Contains(err.Error(), "is a terminal, not an agent") {
		t.Fatalf("Type at a terminal = %v", err)
	}
}
