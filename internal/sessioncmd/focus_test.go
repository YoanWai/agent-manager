package sessioncmd

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// fakeFocusManager answers one request on the harness server's endpoint
// with reply, hands over the request it read, and then stops listening.
func fakeFocusManager(t *testing.T, h *sessionHarness, reply FocusAnswer) <-chan FocusRequest {
	t.Helper()
	listener, err := net.Listen("unix", h.driver.FocusEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	requests := make(chan FocusRequest, 1)
	go func() {
		defer close(requests)
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var request FocusRequest
		if json.NewDecoder(conn).Decode(&request) == nil {
			requests <- request
			_ = json.NewEncoder(conn).Encode(reply)
		}
	}()
	return requests
}

// A request reaches only a manager that is listening, for a session the
// list can show; anything else is refused before anything is sent.
func TestFocusRefusesWhatTheManagerCannotShow(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Focus(worker.ID, true); err == nil || !strings.Contains(err.Error(), "no Agent Manager is running") {
		t.Fatalf("Focus with no manager = %v", err)
	}
	if _, err := h.sessions.Focus("feedface", true); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Focus of an unknown id = %v", err)
	}
	if _, err := h.sessions.Archive(h.caller.ID, worker.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, err := h.sessions.Focus(worker.ID, true); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("Focus of an archived session = %v", err)
	}
}

// The manager's answer is the result: success names the session, and a
// refusal comes back as the error, so the caller never reports more than
// the manager did.
func TestFocusReportsWhatTheManagerAnswered(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	requests := fakeFocusManager(t, h, FocusAnswer{})
	focused, err := h.sessions.Focus(worker.ID, true)
	if err != nil || focused.ID != worker.ID {
		t.Fatalf("Focus = %+v, %v; want the worker", focused, err)
	}
	if got := <-requests; got != (FocusRequest{ID: worker.ID, Enter: true}) {
		t.Fatalf("manager read %+v, want the worker entered", got)
	}
	for range requests {
	}

	refusal := "session " + worker.ID + " is selected under a dialog"
	fakeFocusManager(t, h, FocusAnswer{Error: refusal})
	if _, err := h.sessions.Focus(worker.ID, true); err == nil || err.Error() != refusal {
		t.Fatalf("Focus under a refusal = %v, want %q", err, refusal)
	}
}
