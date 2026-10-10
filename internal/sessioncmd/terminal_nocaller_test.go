package sessioncmd

import (
	"strings"
	"testing"
)

// With no calling session, as over SSH, a terminal opens loose, and only a
// loose one can be driven: a nested terminal stays its parent's.
func TestTerminalsWithNoCallingSessionDriveOnlyLooseTerminals(t *testing.T) {
	h := newSessionHarness(t)
	group := "backend"
	loose, err := h.terminals.Create("", CreateTerminalOptions{Group: &group, Directory: h.caller.Cwd})
	if err != nil {
		t.Fatalf("Create with no caller: %v", err)
	}
	if loose.ParentID != "" || loose.Group != "backend" {
		t.Fatalf("terminal opened with no caller = %+v", loose)
	}
	root, err := h.terminals.Create("", CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create in the root with no caller: %v", err)
	}
	if root.ParentID != "" || root.Group != "" {
		t.Fatalf("terminal opened with no group = %+v", root)
	}
	nest := true
	if _, err := h.terminals.Create("", CreateTerminalOptions{Nest: &nest}); err == nil ||
		!strings.Contains(err.Error(), "no calling session to nest") {
		t.Fatalf("nesting with no caller = %v", err)
	}

	if _, err := h.terminals.Send("", loose.ID, "echo from-afar", nil); err != nil {
		t.Fatalf("Send with no caller: %v", err)
	}
	waitForTerminalOutput(t, h.terminals, "", loose.ID, "from-afar")

	nested, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create nested: %v", err)
	}
	want := "terminal " + nested.ID + " belongs to session " + h.caller.ID + "; only it can drive it"
	if _, err := h.terminals.Send("", nested.ID, "echo hi", nil); err == nil || err.Error() != want {
		t.Fatalf("Send to a nested terminal with no caller = %v", err)
	}
	if _, err := h.terminals.Read("", nested.ID); err == nil || err.Error() != want {
		t.Fatalf("Read of a nested terminal with no caller = %v", err)
	}
	if err := h.terminals.Close("", nested.ID); err == nil || err.Error() != want {
		t.Fatalf("Close of a nested terminal with no caller = %v", err)
	}
	if _, err := h.terminals.Read(h.caller.ID, nested.ID); err != nil {
		t.Fatalf("the parent reading its own terminal: %v", err)
	}

	// A loose terminal a session or the user opened is not one a caller
	// with no session may drive: dropping the session's environment must
	// not reach it.
	unnest := false
	opened, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Nest: &unnest})
	if err != nil {
		t.Fatalf("Create un-nested from a session: %v", err)
	}
	if opened.ParentID != "" {
		t.Fatalf("un-nested terminal = %+v", opened)
	}
	want = "terminal " + opened.ID + " was opened in Agent Manager, not by a caller with no session; only Agent Manager and its sessions can drive it"
	if _, err := h.terminals.Send("", opened.ID, "echo hi", nil); err == nil || err.Error() != want {
		t.Fatalf("Send to a terminal opened in Agent Manager with no caller = %v", err)
	}
	if _, err := h.terminals.Read("", opened.ID); err == nil || err.Error() != want {
		t.Fatalf("Read of a terminal opened in Agent Manager with no caller = %v", err)
	}
	if err := h.terminals.Close("", opened.ID); err == nil || err.Error() != want {
		t.Fatalf("Close of a terminal opened in Agent Manager with no caller = %v", err)
	}
	if _, err := h.terminals.Read(h.caller.ID, opened.ID); err != nil {
		t.Fatalf("a session reading a loose terminal: %v", err)
	}

	if err := h.terminals.Close("", loose.ID); err != nil {
		t.Fatalf("Close with no caller: %v", err)
	}
	if h.driver.Exists(loose.ID) {
		t.Fatal("a closed terminal kept its pane")
	}
	if _, err := h.store.Get(loose.ID); err == nil {
		t.Fatal("a closed terminal kept its row")
	}
}
