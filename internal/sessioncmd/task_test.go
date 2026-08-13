package sessioncmd

import (
	"strings"
	"testing"
)

func TestTasksAreClaimedByExactlyOneSession(t *testing.T) {
	h := newSessionHarness(t)
	rival, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "rival"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created, err := h.sessions.CreateTask(h.caller.ID, "fix the retry backoff", "see internal/retry", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.State != "pending" || created.Blocked {
		t.Fatalf("new task = %+v", created)
	}

	claimed, err := h.sessions.ClaimTask(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed.State != "in_progress" || !claimed.Mine {
		t.Fatalf("claimed task = %+v", claimed)
	}
	// The second agent must be told, not silently allowed to duplicate work.
	if _, err := h.sessions.ClaimTask(rival.ID, created.ID); err == nil ||
		!strings.Contains(err.Error(), "already claimed by calling-agent") {
		t.Fatalf("rival claim error = %v", err)
	}
	if _, err := h.sessions.FinishTask(rival.ID, created.ID); err == nil ||
		!strings.Contains(err.Error(), "not yours") {
		t.Fatalf("rival finish error = %v", err)
	}

	finished, err := h.sessions.FinishTask(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("FinishTask: %v", err)
	}
	if finished.State != "done" {
		t.Fatalf("finished task = %+v", finished)
	}
}

func TestDependenciesGateAClaimUntilTheyAreDone(t *testing.T) {
	h := newSessionHarness(t)
	first, err := h.sessions.CreateTask(h.caller.ID, "add the column", "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	second, err := h.sessions.CreateTask(h.caller.ID, "backfill the column", "", []string{first.ID})
	if err != nil {
		t.Fatalf("CreateTask dependent: %v", err)
	}
	if !second.Blocked {
		t.Fatalf("dependent task should start blocked: %+v", second)
	}
	if _, err := h.sessions.ClaimTask(h.caller.ID, second.ID); err == nil ||
		!strings.Contains(err.Error(), "blocked on") {
		t.Fatalf("blocked claim error = %v", err)
	}
	// Claiming without an id must skip the blocked task and take the ready one.
	next, err := h.sessions.ClaimTask(h.caller.ID, "")
	if err != nil {
		t.Fatalf("ClaimTask next: %v", err)
	}
	if next.ID != first.ID {
		t.Fatalf("claim-next took the blocked task: %+v", next)
	}
	if _, err := h.sessions.FinishTask(h.caller.ID, first.ID); err != nil {
		t.Fatalf("FinishTask: %v", err)
	}
	unblocked, err := h.sessions.ClaimTask(h.caller.ID, "")
	if err != nil {
		t.Fatalf("ClaimTask after the dependency finished: %v", err)
	}
	if unblocked.ID != second.ID {
		t.Fatalf("finishing a dependency did not unblock its dependent: %+v", unblocked)
	}
	if _, err := h.sessions.ClaimTask(h.caller.ID, ""); err == nil ||
		!strings.Contains(err.Error(), "no task is ready") {
		t.Fatalf("empty-list claim error = %v", err)
	}
	if _, err := h.sessions.CreateTask(h.caller.ID, "depends on nothing real", "", []string{"nope1234"}); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown dependency error = %v", err)
	}
}

func TestReleasedAndDeletedTasksLeaveTheList(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.CreateTask(h.caller.ID, "spike the cache", "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := h.sessions.ClaimTask(h.caller.ID, created.ID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	released, err := h.sessions.ReleaseTask(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("ReleaseTask: %v", err)
	}
	if released.State != "pending" || released.Owner != "" {
		t.Fatalf("released task = %+v", released)
	}
	if err := h.sessions.DeleteTask(h.caller.ID, created.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	tasks, err := h.sessions.Tasks(h.caller.ID)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("deleted task is still listed: %+v", tasks)
	}
	if err := h.sessions.DeleteTask(h.caller.ID, created.ID); err == nil {
		t.Fatal("deleting a task twice should report it is gone")
	}
}

func TestDeletingASessionHandsItsClaimsBack(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created, err := h.sessions.CreateTask(h.caller.ID, "migrate the table", "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := h.sessions.ClaimTask(worker.ID, created.ID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if err := h.store.Delete(worker.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	tasks, err := h.sessions.Tasks(h.caller.ID)
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].State != "pending" || tasks[0].Owner != "" {
		t.Fatalf("a deleted session left its claim parked: %+v", tasks)
	}
}
