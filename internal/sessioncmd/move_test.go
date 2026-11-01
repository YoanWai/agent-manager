package sessioncmd

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

func statusLeft(t *testing.T, h *sessionHarness, id string) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", h.driver.SocketName(), "show-options", "-v", "-t", "am_"+id, "status-left").CombinedOutput()
	if err != nil {
		t.Fatalf("read status-left of %s: %v: %s", id, err, out)
	}
	return strings.TrimSpace(string(out))
}

func groupOf(t *testing.T, h *sessionHarness, id string) store.Session {
	t.Helper()
	sess, err := h.store.Get(id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return sess
}

// The agent's terminals follow it, and both status bars name the new group,
// with no calling session, as a script tidying the list runs it.
func TestMoveFilesAnAgentWithItsTerminals(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.CreateGroup("", "repos", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create terminal: %v", err)
	}

	placement, err := h.sessions.Move("", h.caller.ID, "/repos/")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	want := Placement{ID: h.caller.ID, Name: h.caller.Name, Kind: "session", From: "backend", Group: "repos", Terminals: []string{terminal.ID}}
	if placement.ID != want.ID || placement.Kind != want.Kind || placement.From != want.From || placement.Group != want.Group ||
		placement.UnnestedFrom != "" || !slices.Equal(placement.Terminals, want.Terminals) {
		t.Fatalf("placement = %+v, want %+v", placement, want)
	}
	if got := FormatPlacement(placement); got != "moved session calling-agent (id "+h.caller.ID+") from backend to repos; 1 terminal(s) went with it: "+terminal.ID {
		t.Fatalf("text = %q", got)
	}
	if agent := groupOf(t, h, h.caller.ID); agent.Group != "repos" {
		t.Fatalf("agent sits in %q", agent.Group)
	}
	if moved := groupOf(t, h, terminal.ID); moved.Group != "repos" || moved.ParentID != h.caller.ID {
		t.Fatalf("terminal sits in %q under %q", moved.Group, moved.ParentID)
	}
	if label := statusLeft(t, h, h.caller.ID); label != "repos · calling-agent" {
		t.Fatalf("agent status bar = %q", label)
	}
	if label := statusLeft(t, h, terminal.ID); label != "repos · "+terminal.Name {
		t.Fatalf("terminal status bar = %q", label)
	}
}

func TestMoveUnnestsATerminalMovedAlone(t *testing.T) {
	h := newSessionHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create terminal: %v", err)
	}

	placement, err := h.sessions.Move(h.caller.ID, terminal.ID, "")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if placement.Kind != "terminal" || placement.Group != "" || placement.UnnestedFrom != h.caller.ID {
		t.Fatalf("placement = %+v", placement)
	}
	if moved := groupOf(t, h, terminal.ID); moved.Group != "" || moved.ParentID != "" {
		t.Fatalf("terminal sits in %q under %q, want the root un-nested", moved.Group, moved.ParentID)
	}
	if label := statusLeft(t, h, terminal.ID); label != terminal.Name {
		t.Fatalf("terminal status bar = %q", label)
	}
}

// Running the same tidy twice changes nothing, the order included.
func TestMoveIntoTheGroupItIsInChangesNothing(t *testing.T) {
	h := newStoreSessionHarness(t)
	later := h.addSessionRow(t, "later")
	if err := h.store.MoveSession(later, "backend"); err != nil {
		t.Fatalf("MoveSession: %v", err)
	}

	placement, err := h.sessions.Move(h.caller.ID, h.caller.ID, "backend")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if !placement.Unchanged {
		t.Fatalf("placement = %+v, want unchanged", placement)
	}
	if got := FormatPlacement(placement); got != "session calling-agent (id "+h.caller.ID+") is already in backend" {
		t.Fatalf("text = %q", got)
	}
	listed, err := h.store.ListSessions(true)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var order []string
	for _, sess := range listed {
		order = append(order, sess.ID)
	}
	if !slices.Equal(order, []string{h.caller.ID, later}) {
		t.Fatalf("order = %v, want the caller still first", order)
	}
}

func TestMoveRefusesWhatItCannotFile(t *testing.T) {
	h := newStoreSessionHarness(t)
	if err := h.store.CreateGroup("old", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := h.store.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := h.store.SetGroupArchived("old", true); err != nil {
		t.Fatalf("SetGroupArchived: %v", err)
	}
	for _, tc := range []struct{ target, group, want string }{
		{"", "backend", "session_id is empty; call list_sessions to get one"},
		{"deadbeef", "backend", "session deadbeef does not exist; call list_sessions for current ids"},
		{h.caller.ID, "ghost", `group "ghost" does not exist; call list_groups for the existing ones or create_group to add it`},
		{h.caller.ID, "old/inner", `group "old/inner" is archived; restore it in Agent Manager first`},
	} {
		if _, err := h.sessions.Move(h.caller.ID, tc.target, tc.group); err == nil || err.Error() != tc.want {
			t.Fatalf("Move(%q, %q) = %v, want %q", tc.target, tc.group, err, tc.want)
		}
	}
	if after := groupOf(t, h, h.caller.ID); after.Group != "backend" {
		t.Fatalf("a refused move still filed the session in %q", after.Group)
	}
}

// An archived row may be refiled; it stays archived under its new group.
func TestMoveKeepsAnArchivedSessionArchived(t *testing.T) {
	h := newStoreSessionHarness(t)
	id := h.addSessionRow(t, "finished")
	if err := h.store.SetArchived(id, true); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}

	if _, err := h.sessions.Move(h.caller.ID, id, "backend"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if after := groupOf(t, h, id); after.Group != "backend" || !after.Archived {
		t.Fatalf("archived session = group %q archived %t", after.Group, after.Archived)
	}
}
