package link

import (
	"context"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

func TestRunTurnsEachCallIntoItsHostCommand(t *testing.T) {
	for _, tc := range []struct {
		op   Op
		want []string
	}{
		{Type, []string{"type", "--json", "--", "s1", "hello"}},
		{Kill, []string{"kill", "--json", "--", "s1"}},
		{Revive, []string{"revive", "--json", "--", "s1"}},
		{Archive, []string{"archive", "--json", "--", "s1"}},
		{Restore, []string{"archive", "--restore", "--json", "--", "s1"}},
	} {
		fake := &ssh{}
		client := listedState(t, fake).Client
		call := Call{Op: tc.op, Host: "box", Ref: remote.Ref{Host: "box", ID: "s1"}, Name: "api", Text: "hello"}
		if _, err := Run(context.Background(), client, nil, call); err != nil {
			t.Fatalf("%v: %v", tc.op, err)
		}
		if calls := fake.taken(); len(calls) != 1 || !equal(calls[0], tc.want) {
			t.Fatalf("%v: calls %q, want %q", tc.op, calls, tc.want)
		}
	}
}

func TestRunSpawnsAndOpensTerminals(t *testing.T) {
	fake := &ssh{}
	client := listedState(t, fake).Client
	group := "web"
	call := Call{Op: Spawn, Host: "box", Spawn: sessioncmd.CreateSessionOptions{Tool: "claude", Group: &group, Prompt: "go"}}
	result, err := Run(context.Background(), client, nil, call)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if result.Session.Name != "fresh" {
		t.Fatalf("spawn result = %+v", result.Session)
	}
	call = Call{Op: NewTerminal, Host: "box", Terminal: sessioncmd.CreateTerminalOptions{Group: &group}}
	if _, err := Run(context.Background(), client, nil, call); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	calls := fake.taken()
	if len(calls) != 2 || calls[0][0] != "spawn" || calls[1][0] != "terminal" {
		t.Fatalf("calls = %q", calls)
	}
}

// A call that failed says why and reads the host again, so its rows are
// what the host holds now.
func TestAFailedCallReportsAndPicksTheHostBackUp(t *testing.T) {
	fake := &ssh{}
	state := listedState(t, fake)
	root := &recorder{}
	failed := &remote.Error{Host: "box", Err: context.DeadlineExceeded}
	call := Call{Op: Type, Host: "box", Ref: remote.Ref{Host: "box", ID: "s1"}, Name: "api", Draft: "hello", ComposerGen: 3}
	if cmd := state.ApplyCall(root, call, Result{}, failed); cmd == nil {
		t.Fatal("a failed call did not pick the host back up")
	}
	if want := []string{failed.Error()}; !equal(root.errs, want) {
		t.Fatalf("errors = %q, want %q", root.errs, want)
	}
}

func TestASpawnFromTheFormClosesItAndAQuickSpawnClearsTheBar(t *testing.T) {
	state := listedState(t, &ssh{})
	root := &recorder{}
	state.ApplyCall(root, Call{Op: Spawn, Host: "box", FromForm: true, ComposerGen: 4}, Result{Session: sessioncmd.Session{Name: "fresh"}}, nil)
	if len(root.closed) != 1 || root.closed[0] != FormAsker {
		t.Fatalf("closed = %v, want the form", root.closed)
	}
	root = &recorder{}
	state.ApplyCall(root, Call{Op: Spawn, Host: "box", ComposerGen: 5, Draft: "go"}, Result{Session: sessioncmd.Session{Name: "fresh"}}, nil)
	if root.cleared != 1 || len(root.closed) != 0 {
		t.Fatalf("cleared %d, closed %v", root.cleared, root.closed)
	}
	if want := "started fresh on box"; len(root.done) != 1 || root.done[0] != want {
		t.Fatalf("status = %q, want %q", root.done, want)
	}
}

func equal(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
