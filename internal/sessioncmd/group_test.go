package sessioncmd

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSessionGroupsListAndCreate(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.CreateGroup(h.caller.ID, "backend/payments", h.caller.Cwd)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if created.Path != "backend/payments" || !sameTerminalPath(created.Directory, h.caller.Cwd) {
		t.Fatalf("created group = %+v", created)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "backend/payments", ""); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate group error = %v", err)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "unknown/child", ""); err == nil ||
		!strings.Contains(err.Error(), "parent group") {
		t.Fatalf("orphan group error = %v", err)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "  ", ""); err == nil {
		t.Fatal("an empty group path should be refused")
	}

	spawned, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Group: &created.Path})
	if err != nil {
		t.Fatalf("Create in new group: %v", err)
	}
	if spawned.Group != "backend/payments" {
		t.Fatalf("spawn group = %q", spawned.Group)
	}
	groups, err := h.sessions.Groups(h.caller.ID)
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	counts := map[string]int{}
	for _, group := range groups {
		counts[group.Path] = group.Sessions
	}
	if counts["backend"] != 1 || counts["backend/payments"] != 1 {
		t.Fatalf("group counts = %+v", counts)
	}
}

// A fleet that opened a group for its work has to be able to close it, and
// the sessions still filed there are not what it asked to remove.
func TestDeleteGroupMovesItsSessionsToTheRoot(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.CreateGroup(h.caller.ID, "fleet", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	group := "fleet"
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker", Group: &group})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if worker.Group != "fleet" {
		t.Fatalf("session landed in %q, not the group it was given", worker.Group)
	}

	removal, err := h.sessions.DeleteGroup(h.caller.ID, "fleet")
	if err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if len(removal.Removed) != 1 || removal.Removed[0] != "fleet" {
		t.Fatalf("removed = %v", removal.Removed)
	}
	if len(removal.Moved) != 1 || removal.Moved[0] != worker.ID {
		t.Fatalf("moved = %v, want the session that was filed there", removal.Moved)
	}

	// The session is the point: it keeps running, at the root.
	after, err := h.store.Get(worker.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Group != "" {
		t.Fatalf("session sits in %q rather than the root", after.Group)
	}
	if !h.driver.Exists(worker.ID) {
		t.Fatal("deleting a group stopped the agent running in it")
	}
	groups, err := h.sessions.Groups(h.caller.ID)
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	for _, g := range groups {
		if g.Path == "fleet" {
			t.Fatal("the group survived its deletion")
		}
	}
	if _, err := h.sessions.DeleteGroup(h.caller.ID, "fleet"); err == nil {
		t.Fatal("deleting a group that does not exist was accepted")
	}
}

func TestDeleteGroupTakesItsSubtreeAndKeepsNesting(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.CreateGroup(h.caller.ID, "fleet", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "fleet/backend", ""); err != nil {
		t.Fatalf("CreateGroup nested: %v", err)
	}
	group := "fleet/backend"
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker", Group: &group})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	shell := store.Session{
		ID: "aaaa1111", Name: "sh-worker", Tool: "resting",
		Group: "fleet/backend", Status: status.Idle, ParentID: worker.ID,
	}
	if err := h.store.CreateSession(shell); err != nil {
		t.Fatalf("nest shell: %v", err)
	}

	removal, err := h.sessions.DeleteGroup(h.caller.ID, "fleet")
	if err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if len(removal.Removed) != 2 {
		t.Fatalf("removed = %v, want the group and its child", removal.Removed)
	}
	if len(removal.Moved) != 2 {
		t.Fatalf("moved = %v, want both sessions from the nested group", removal.Moved)
	}
	after, err := h.store.Get(shell.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Group != "" {
		t.Fatalf("nested session sits in %q rather than the root", after.Group)
	}
	if after.ParentID != worker.ID {
		t.Fatalf("moving the subtree unhooked the terminal from its agent: parent %q", after.ParentID)
	}
}
