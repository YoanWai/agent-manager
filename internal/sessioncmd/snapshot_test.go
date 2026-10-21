package sessioncmd

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
)

func TestSnapshotHoldsWhatTheListsHoldPlusArchivedRows(t *testing.T) {
	h := newSessionHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	archivedAgent := store.Session{ID: uuid.NewString()[:8], Name: "done-agent", Tool: "echoer", Cwd: h.caller.Cwd, Group: "backend", Status: status.Idle, Archived: true}
	archivedShell := store.Session{ID: uuid.NewString()[:8], Name: "old-shell", Tool: "terminal", Cwd: h.caller.Cwd, Group: "backend", Status: status.Idle, Archived: true, ParentID: archivedAgent.ID}
	for _, sess := range []store.Session{archivedAgent, archivedShell} {
		if err := h.store.CreateSession(sess); err != nil {
			t.Fatalf("create %s: %v", sess.Name, err)
		}
	}

	snapshot, err := h.sessions.Snapshot(h.caller.ID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.Version != SnapshotVersion {
		t.Fatalf("version = %d", snapshot.Version)
	}
	listed, err := h.sessions.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Sessions, listed) {
		t.Fatalf("snapshot sessions = %+v\nlist = %+v", snapshot.Sessions, listed)
	}
	if !slices.ContainsFunc(snapshot.Sessions, func(s Session) bool { return s.ID == archivedAgent.ID && s.Archived && !s.Running }) {
		t.Fatalf("archived agent missing from %+v", snapshot.Sessions)
	}
	live, err := h.terminals.List(h.caller.ID)
	if err != nil {
		t.Fatalf("terminal List: %v", err)
	}
	wantTerminals := append(live, Terminal{
		ID: archivedShell.ID, Name: "old-shell", Group: "backend", Directory: h.caller.Cwd, Status: status.Idle,
		ParentID: archivedAgent.ID, ParentName: "done-agent", Archived: true,
	})
	if !reflect.DeepEqual(snapshot.Terminals, wantTerminals) {
		t.Fatalf("snapshot terminals = %+v\nwant %+v", snapshot.Terminals, wantTerminals)
	}
	if snapshot.Terminals[0].ID != terminal.ID {
		t.Fatalf("live terminal not first: %+v", snapshot.Terminals)
	}
	groups, err := h.sessions.Groups(h.caller.ID)
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Groups, groups) {
		t.Fatalf("snapshot groups = %+v, groups = %+v", snapshot.Groups, groups)
	}
	if snapshot.ManagerAwake {
		t.Fatal("no manager has stamped the heartbeat, so none is awake")
	}
}

func TestSnapshotWithNoCallerReportsAnAwakeManager(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.store.ClaimPoller(h.driver.SocketPath(), time.Now()); err != nil {
		t.Fatalf("ClaimPoller: %v", err)
	}
	snapshot, err := h.sessions.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot with no caller: %v", err)
	}
	if !snapshot.ManagerAwake {
		t.Fatal("a fresh heartbeat should read as an awake manager")
	}
	if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Self {
		t.Fatalf("sessions with no caller = %+v", snapshot.Sessions)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if string(keys["terminals"]) != "[]" {
		t.Fatalf("no terminals should encode as an empty list, got %s", keys["terminals"])
	}
	for _, key := range []string{"version", "manager_awake", "sessions", "terminals", "groups"} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("envelope lacks %q: %s", key, raw)
		}
	}
}
