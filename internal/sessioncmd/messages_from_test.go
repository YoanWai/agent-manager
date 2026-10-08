package sessioncmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

// A caller on another machine has no session here, so the row it queues
// carries only the name it gave, and the manager types the envelope that
// says no reply can reach it.
func TestSendFromQueuesUnderTheGivenNameWithNoSenderSession(t *testing.T) {
	h := newSessionHarness(t)
	sent, err := h.sessions.SendFrom("", "laptop-agent", h.caller.ID, "rebase on main")
	if err != nil {
		t.Fatalf("SendFrom: %v", err)
	}
	if sent.MessageID == 0 || sent.QueuePosition != 1 {
		t.Fatalf("send result = %+v", sent)
	}
	head, queued, err := h.store.HeadMessage(h.caller.ID)
	if err != nil || !queued {
		t.Fatalf("HeadMessage: %v, queued=%v", err, queued)
	}
	if head.ID != sent.MessageID || head.SenderID != "" || head.SenderName != "laptop-agent" || head.Body != "rebase on main" {
		t.Fatalf("queued row = %+v", head)
	}
	if _, err := h.sessions.SendFrom("", "laptop-agent", h.caller.ID, "rebase on main"); !errors.Is(err, store.ErrInboxDuplicate) {
		t.Fatalf("a repeated message = %v, want the duplicate refused", err)
	}
	if _, err := h.sessions.SendFrom("", strings.Repeat("n", maxSenderNameBytes), h.caller.ID, "a name at the limit"); err != nil {
		t.Fatalf("a name at the limit: %v", err)
	}
}

func TestSendFromRefusesACallingSessionAndAnUnusableName(t *testing.T) {
	h := newSessionHarness(t)
	worker := h.addSessionRow(t, "worker")
	if _, err := h.sessions.SendFrom(worker, "laptop-agent", h.caller.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "session "+worker) {
		t.Fatalf("--from from inside a session = %v, want the calling session named", err)
	}
	for _, name := range []string{"", "   ", strings.Repeat("n", maxSenderNameBytes+1), "two\nlines", "bell\a", "esc\x1b[31m", "del\x7f", "c1\u0085"} {
		if _, err := h.sessions.SendFrom("", name, h.caller.ID, "rebase on main"); err == nil {
			t.Fatalf("sender name %q was accepted", name)
		}
	}
	if queued, err := h.store.QueuedCount(h.caller.ID); err != nil || queued != 0 {
		t.Fatalf("a refused send queued %d messages (%v)", queued, err)
	}
}
