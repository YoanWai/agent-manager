package sessioncmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestWaitReturnsAsSoonAsTheSessionRests(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Create leaves the row on starting; the manager would move it on. Here
	// the test plays the manager.
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = h.store.UpdateStatus(created.ID, status.Finished)
	}()

	started := time.Now()
	result, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Reached || result.Session.Status != status.Finished {
		t.Fatalf("wait result = %+v", result)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("wait did not return promptly: %s", elapsed)
	}
}

func TestWaitTimesOutWithTheCurrentStateRatherThanAnError(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Working); err != nil {
		t.Fatal(err)
	}
	// Shorter than the poll interval, so a wait that only wakes on the tick
	// overruns the timeout its caller asked for.
	asked := 300 * time.Millisecond
	started := time.Now()
	result, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, nil, asked)
	if err != nil {
		t.Fatalf("a timeout must not be an error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("a %s wait took %s", asked, elapsed)
	}
	if result.Reached || result.Outcome != WaitTimedOut || result.Session.Status != status.Working {
		t.Fatalf("timeout result = %+v", result)
	}
	for _, timeout := range []time.Duration{-time.Second, MaxWaitTimeout + time.Second} {
		if _, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, nil, timeout); err == nil ||
			!strings.Contains(err.Error(), "outside 0 to "+MaxWaitTimeout.String()) {
			t.Fatalf("a %s wait = %v, want a refusal naming the bound", timeout, err)
		}
	}
}

func TestWaitSeesAKilledSessionAsDeadWithoutTheManager(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Working); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Kill(created.ID); err != nil {
		t.Fatal(err)
	}
	result, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, []string{"dead"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Reached || result.Session.Running {
		t.Fatalf("killed session result = %+v", result)
	}
}

// A worker that crashes mid-turn is dead from the first tick, and its
// stored status will never move again. Parking the whole timeout to say
// "timed out" hides a death the wait had already seen.
func TestWaitReportsAnObservedDeathWithoutWaitingOut(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Working); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Kill(created.ID); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, []string{"finished"}, 10*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Outcome != WaitDied || result.Reached {
		t.Fatalf("a session that died while working = %+v", result)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("the wait sat on a death it could already see for %s", elapsed)
	}
	// Waiting for the death itself is still an ordinary arrival.
	awaited, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, []string{"dead"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait for dead: %v", err)
	}
	if awaited.Outcome != WaitReached || !awaited.Reached {
		t.Fatalf("awaiting dead = %+v", awaited)
	}
}

func TestWaitRefusesSelfAndUnknownStates(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Wait(context.Background(), h.caller.ID, h.caller.ID, nil, time.Second); err == nil ||
		!strings.Contains(err.Error(), "cannot wait on itself") {
		t.Fatalf("self wait error = %v", err)
	}
	if _, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, []string{"done"}, time.Second); err == nil ||
		!strings.Contains(err.Error(), "unknown state") {
		t.Fatalf("unknown state error = %v", err)
	}
}

func TestWaitHonoursCancellation(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Working); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	if _, err := h.sessions.Wait(ctx, h.caller.ID, created.ID, nil, time.Minute); err == nil {
		t.Fatal("a cancelled wait should report the cancellation")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancellation was not honoured promptly: %s", elapsed)
	}
}

func TestWaitSeparatesADeathFromATimeout(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Kill(created.ID); err != nil {
		t.Fatal(err)
	}
	// The stored status says finished, which is awaited, but the pane is gone.
	result, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, []string{"finished"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reached || result.Outcome != WaitDied {
		t.Fatalf("a session that died before the awaited state = %+v", result)
	}
}

// A message is typed in only once its recipient rests, and the turn it
// starts shows a poll after that. The rest the row reads until then belongs
// to the turn before, so the sender's wait for its handoff must look past it.
func TestWaitLooksPastTheRestItsOwnQueuedMessageIsAboutToEnd(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, worker.ID, "❯")
	if err := h.store.UpdateStatus(worker.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "run the migration")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	result, err := h.sessions.Wait(context.Background(), h.caller.ID, worker.ID, nil, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reached || result.Outcome != WaitTimedOut || result.Session.Status != status.Finished {
		t.Fatalf("the wait took the rest before its own message was typed in: %+v", result)
	}

	// Typed in, with the pass that did it yet to write the status: the row
	// still holds the rest it read before typing.
	if err := h.store.MarkDelivered(sent.MessageID, time.Now()); err != nil {
		t.Fatal(err)
	}
	result, err = h.sessions.Wait(context.Background(), h.caller.ID, worker.ID, nil, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Reached || result.Outcome != WaitTimedOut {
		t.Fatalf("the wait took a status written before its message was typed in: %+v", result)
	}

	// The turn it started has ended.
	if err := h.store.UpdateStatus(worker.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	result, err = h.sessions.Wait(context.Background(), h.caller.ID, worker.ID, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Reached || result.Session.Status != status.Finished {
		t.Fatalf("a delivered message still held the wait: %+v", result)
	}
}

// A message the manager gave up on never reached the prompt, so it started
// no turn for the wait to look past.
func TestWaitTakesTheRestAfterItsOwnMessageWasDropped(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.UpdateStatus(worker.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "run the migration")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := h.store.MarkDropped(sent.MessageID, time.Now()); err != nil {
		t.Fatal(err)
	}

	result, err := h.sessions.Wait(context.Background(), h.caller.ID, worker.ID, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Reached || result.Session.Status != status.Finished {
		t.Fatalf("a dropped message held the wait: %+v", result)
	}
}

// Only the caller's own message says its handoff has not landed. Another
// session's traffic to the same recipient is for that session to wait on.
func TestWaitTakesTheRestWhileAnotherSendersMessageIsQueued(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, worker.ID, "❯")
	if err := h.store.UpdateStatus(worker.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Enqueue(store.InboxMessage{
		SessionID:   worker.ID,
		SenderID:    "other001",
		SenderName:  "another-coordinator",
		Body:        "run the migration",
		Fingerprint: "run the migration",
		SentAt:      time.Now(),
	}, store.DefaultInboxLimits); err != nil {
		t.Fatal(err)
	}

	result, err := h.sessions.Wait(context.Background(), h.caller.ID, worker.ID, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Reached || result.Session.Status != status.Finished {
		t.Fatalf("another sender's message held this caller's wait: %+v", result)
	}
}

// A message the manager will not type in never starts the turn the wait
// would look past, so the recipient's state is the answer: here, the dialog
// the caller has to get answered first.
func TestWaitTakesTheDialogThatHoldsItsOwnMessage(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "dialog", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, worker.ID, "Enter to confirm")
	if err := h.store.UpdateStatus(worker.ID, status.Waiting); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Send(h.caller.ID, worker.ID, "run the migration"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	result, err := h.sessions.Wait(context.Background(), h.caller.ID, worker.ID, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Reached || result.Session.Status != status.Waiting {
		t.Fatalf("a message held by a dialog kept the wait from the dialog: %+v", result)
	}
}
