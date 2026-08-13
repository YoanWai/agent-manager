package sessioncmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
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
	result, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, nil, time.Second)
	if err != nil {
		t.Fatalf("a timeout must not be an error: %v", err)
	}
	if result.Reached || result.Session.Status != status.Working {
		t.Fatalf("timeout result = %+v", result)
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
