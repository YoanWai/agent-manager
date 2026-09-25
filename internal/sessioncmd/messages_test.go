package sessioncmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSessionsSendAndReadReachTheTargetPane(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent.MessageID == 0 || sent.QueuePosition != 1 {
		t.Fatalf("send result = %+v", sent)
	}
	// The message is queued, not typed: nothing reaches the pane until a
	// running manager decides the target is at rest.
	screen, err := h.sessions.Read(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if strings.Contains(screen.Output, "rebase on main") {
		t.Fatalf("send must not type into the pane itself, got %q", screen.Output)
	}
	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "queued" {
		t.Fatalf("message state = %+v", state)
	}
	if _, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main"); err == nil {
		t.Fatal("an identical message should be refused as a duplicate")
	}
	if _, err := h.sessions.Send(h.caller.ID, h.caller.ID, "talking to myself"); err == nil {
		t.Fatal("a session should not message itself")
	}

	if _, err := h.sessions.Send(h.caller.ID, created.ID, "   "); err == nil {
		t.Fatal("an empty message should be refused")
	}
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, terminal.ID, "ls"); err == nil ||
		!strings.Contains(err.Error(), "terminal, not an agent") {
		t.Fatalf("sending to a terminal error = %v", err)
	}
}

func TestSendAndWaitRefuseATargetTheManagerNoLongerPolls(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Archive(h.caller.ID, created.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// The pane is still alive, so nothing else would catch this: an archived
	// row is skipped by the poller, and the message would queue forever.
	if _, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "archived") {
		t.Fatalf("send to an archived session = %v", err)
	}
	if _, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, nil, time.Second); err == nil ||
		!strings.Contains(err.Error(), "archived") {
		t.Fatalf("wait on an archived session = %v", err)
	}
}

func TestSendRefusesAToolTheManagerCannotReadReadinessFrom(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "blind", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "marks no input box") {
		t.Fatalf("send to a tool with no readiness marker = %v", err)
	}
}

// Whether a manager is awake is read off a heartbeat only the poller
// writes. A value that is not a timestamp means something else wrote that
// row, and reporting it as "no manager" would send the caller after the
// wrong problem.
func TestAnUnreadableHeartbeatIsReportedRatherThanReadAsAClosedManager(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.SetSetting(store.PollerHeartbeatKey, "just now"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "poller heartbeat") {
		t.Fatalf("Send with a corrupt heartbeat = %v", err)
	}
}

// A sender follows its own message instead of reading the recipient's
// screen, and the recipient answering is the acknowledgement. Both
// transitions belong to this front; the store tests cover the rows they
// write, and nothing follows one message across the two.
func TestASenderSeesItsMessageDeliveredThenAnswered(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The queued row is the whole contract with the manager: it names the
	// sender in the envelope it types and claims the row before typing.
	head, queued, err := h.store.HeadMessage(worker.ID)
	if err != nil || !queued {
		t.Fatalf("HeadMessage: %v, queued=%v", err, queued)
	}
	if head.ID != sent.MessageID || head.SenderID != h.caller.ID || head.SenderName != h.caller.Name ||
		head.Body != "rebase on main" || !head.ClaimedAt.IsZero() {
		t.Fatalf("queued row = %+v, caller = %+v", head, h.caller)
	}

	// The manager's poller owns delivery; these are the two writes it makes
	// once it finds the target at rest.
	claimed, err := h.store.ClaimMessage(sent.MessageID, time.Now())
	if err != nil || !claimed {
		t.Fatalf("ClaimMessage: %v, claimed=%v", err, claimed)
	}
	if err := h.store.MarkDelivered(sent.MessageID, time.Now()); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "delivered" || state.DeliveredAt == "" || state.SessionID != worker.ID {
		t.Fatalf("delivered state = %+v", state)
	}

	if _, err := h.sessions.Send(worker.ID, h.caller.ID, "rebased, tests pass"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus after the reply: %v", err)
	}
	if state.State != "answered" {
		t.Fatalf("a reply did not acknowledge the message it answers: %+v", state)
	}
	// Only the sender may follow it; another session asking is told so
	// rather than shown someone else's traffic.
	if _, err := h.sessions.MessageStatus(worker.ID, sent.MessageID); err == nil ||
		!strings.Contains(err.Error(), "was not sent by this session") {
		t.Fatalf("reading another session's message = %v", err)
	}
}

// A message the manager could not type is retired so nothing retypes it,
// which leaves it looking exactly like a delivered one in the queue. The
// sender has no other way to find out it never landed.
func TestASenderIsToldWhenItsMessageWasDropped(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := h.store.MarkDropped(sent.MessageID, time.Now()); err != nil {
		t.Fatalf("MarkDropped: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "dropped" || state.DeliveredAt != "" {
		t.Fatalf("dropped message reported as %+v", state)
	}
	if !strings.Contains(state.Reason, "send it again") {
		t.Fatalf("a dropped message does not say what to do about it: %+v", state)
	}
	// A reply must not turn a message that never arrived into an answered one.
	if _, err := h.sessions.Send(worker.ID, h.caller.ID, "rebased, tests pass"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus after the reply: %v", err)
	}
	if state.State != "dropped" {
		t.Fatalf("a dropped message was acknowledged by an unrelated reply: %+v", state)
	}
}

// A sender is told a hold only where the manager keeps one. The gate is the
// recipient tool's own rules, read off its current screen: a dialog holds
// the queue, because text typed onto one picks an option, and a prompt at
// rest does not, whatever the stored status says about it.
func TestASenderSeesAMessageHeldByARecipientOnADialog(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "dialog", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, worker.ID, "Enter to confirm")

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "held" {
		t.Fatalf("a message behind a dialog reads as %+v", state)
	}
	if !strings.Contains(state.Reason, worker.ID) || !strings.Contains(state.Reason, "dialog") {
		t.Fatalf("the hold does not say why: %+v", state)
	}

	// A session whose screen shows no dialog is delivered to, so its sender
	// hears the truth: ordinary queued, whatever waiting the row carries.
	resting, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "resting-worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	restingSend, err := h.sessions.Send(h.caller.ID, resting.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, resting.ID, "❯")
	if err := h.store.UpdateStatus(resting.ID, status.Waiting); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, restingSend.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "queued" || state.Reason != "" {
		t.Fatalf("a recipient the manager will type into was reported as holding its queue: %+v", state)
	}
}

// A recipient can leave the manager's reach after the send: archived rows
// are skipped by the poll, and a dead session has no pane to type into. The
// queue then never moves, and the sender is the one who has to be told.
func TestASenderIsToldWhenItsRecipientLeftTheManagersReach(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := h.sessions.Archive(h.caller.ID, worker.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "held" || !strings.Contains(state.Reason, "archived") ||
		!strings.Contains(state.Reason, h.sessions.words.Restore) {
		t.Fatalf("a message to an archived session reads as %+v", state)
	}

	if _, err := h.sessions.Archive(h.caller.ID, worker.ID, false); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := h.sessions.Kill(h.caller.ID, worker.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "held" || !strings.Contains(state.Reason, "not running") ||
		!strings.Contains(state.Reason, h.sessions.words.Revive) {
		t.Fatalf("a message to a dead session reads as %+v", state)
	}
}

// An errored session is running, unarchived and configured, so every other
// held case passes it by, while the poller types into resting sessions only.
// A coordinator polling a handoff has to be able to tell that apart from a
// recipient that is merely slow.
func TestASenderIsToldWhenItsRecipientErrored(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := h.store.UpdateStatus(worker.ID, status.Errored); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "held" || !strings.Contains(state.Reason, "errored") ||
		!strings.Contains(state.Reason, h.sessions.words.Read) {
		t.Fatalf("a message to an errored session reads as %+v", state)
	}
}

// A tool block can be deleted after a message was queued for a session
// running that tool, which leaves the poller unable to read readiness.
// The message stays queued rather than being dropped.
func TestASenderIsToldWhenTheRecipientsToolIsOneThisBuildDoesNotShip(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	runtime, err := h.sessions.open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer runtime.store.Close()
	delete(runtime.cfg.Tools, "resting")

	reason, err := runtime.heldReason(worker.ID)
	if err != nil {
		t.Fatalf("heldReason: %v", err)
	}
	if !strings.Contains(reason, "not a CLI it supports") || !strings.Contains(reason, worker.ID) {
		t.Fatalf("a message whose tool this build does not ship reads as %q", reason)
	}
	if sent.MessageID == 0 {
		t.Fatalf("Send returned no message id")
	}
}

// The queue and rate caps count messages, so without a size cap one message
// is an unbounded paste into another agent's prompt.
func TestSendRefusesAMessageTooLargeToPasteIntoAPrompt(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, worker.ID, strings.Repeat("x", maxMessageBytes+1)); err == nil ||
		!strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("an oversized message was answered with %v", err)
	}
	if queued, err := h.store.QueuedCount(worker.ID); err != nil || queued != 0 {
		t.Fatalf("queued = %d, %v: the refusal still cost the recipient a slot", queued, err)
	}
	if _, err := h.sessions.Send(h.caller.ID, worker.ID, strings.Repeat("x", maxMessageBytes)); err != nil {
		t.Fatalf("a message at the limit was refused: %v", err)
	}
}

func TestSendRefusesAPingPongBetweenAPair(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := range 4 {
		if _, err := h.sessions.Send(h.caller.ID, worker.ID, fmt.Sprintf("from-caller-%d", i)); err != nil {
			t.Fatalf("caller -> worker %d: %v", i, err)
		}
		if _, err := h.sessions.Send(worker.ID, h.caller.ID, fmt.Sprintf("from-worker-%d", i)); err != nil {
			t.Fatalf("worker -> caller %d: %v", i, err)
		}
	}
	_, err = h.sessions.Send(h.caller.ID, worker.ID, "ninth")
	if !errors.Is(err, store.ErrInboxPairLimited) {
		t.Fatalf("9th between the pair = %v, want ErrInboxPairLimited", err)
	}
}
