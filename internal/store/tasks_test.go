package store

import (
	"testing"
	"time"
)

func createTask(t *testing.T, st *Store, id string, dependsOn ...string) {
	t.Helper()
	now := time.Now()
	if err := st.CreateTask(Task{
		ID: id, Title: "work on " + id, State: TaskPending,
		DependsOn: dependsOn, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateTask(%s): %v", id, err)
	}
}

func edgeCount(t *testing.T, st *Store) int {
	t.Helper()
	var edges int
	if err := st.db.QueryRow(`SELECT count(*) FROM task_deps`).Scan(&edges); err != nil {
		t.Fatalf("count task_deps: %v", err)
	}
	return edges
}

func TestDeleteTaskTakesTheEdgesNamingItAlong(t *testing.T) {
	st := newTestStore(t)
	createTask(t, st, "groundwork")
	createTask(t, st, "feature", "groundwork")
	createTask(t, st, "release", "feature")

	deleted, err := st.DeleteTask("feature")
	if err != nil || !deleted {
		t.Fatalf("DeleteTask = %t, %v", deleted, err)
	}
	if edges := edgeCount(t, st); edges != 0 {
		t.Fatalf("both edges named the deleted task, %d survived", edges)
	}
	tasks, err := st.Tasks()
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks after the delete = %+v", tasks)
	}
	for _, task := range tasks {
		if len(task.DependsOn) != 0 {
			t.Fatalf("%s still depends on %v", task.ID, task.DependsOn)
		}
	}
}

// The id is whatever an agent typed, and a task may name a dependency that
// was never created, so a delete that matches no task has to leave the list
// exactly as it found it rather than quietly unblocking the work.
func TestDeleteTaskLeavesTheEdgesOfAnIDThatNamesNoTask(t *testing.T) {
	st := newTestStore(t)
	createTask(t, st, "feature", "groundwork")

	deleted, err := st.DeleteTask("groundwork")
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if deleted {
		t.Fatal("an id naming no task reported a deletion")
	}
	if edges := edgeCount(t, st); edges != 1 {
		t.Fatalf("edges after a delete that matched nothing = %d, want 1", edges)
	}
	blocked, err := st.Task("feature")
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if len(blocked.DependsOn) != 1 || blocked.DependsOn[0] != "groundwork" {
		t.Fatalf("the surviving task lost what it waits on: %+v", blocked)
	}
	claimed, err := st.ClaimTask("feature", "session01", time.Now())
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed {
		t.Fatal("a task the list reports blocked was handed out by id")
	}
	next, taken, err := st.ClaimNextTask("session01", time.Now())
	if err != nil {
		t.Fatalf("ClaimNextTask: %v", err)
	}
	if taken {
		t.Fatalf("a task the list reports blocked was handed out as the next one: %+v", next)
	}
}
