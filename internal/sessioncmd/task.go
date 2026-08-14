package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
)

type Task struct {
	ID        string   `json:"id" jsonschema:"task id; pass it to claim_task, finish_task or release_task"`
	Title     string   `json:"title" jsonschema:"one-line summary of the work"`
	Body      string   `json:"body,omitempty" jsonschema:"full instruction for whoever claims it"`
	State     string   `json:"state" jsonschema:"pending, in_progress or done"`
	Owner     string   `json:"owner,omitempty" jsonschema:"session id holding the claim"`
	OwnerName string   `json:"owner_name,omitempty" jsonschema:"name of the session holding the claim"`
	DependsOn []string `json:"depends_on,omitempty" jsonschema:"task ids that must be done before this one can be claimed"`
	BlockedBy []string `json:"blocked_by,omitempty" jsonschema:"the dependencies that are not done yet; the rest of depends_on are finished and in nobody's way"`
	Blocked   bool     `json:"blocked" jsonschema:"whether unfinished dependencies currently stop this task being claimed"`
	Mine      bool     `json:"mine" jsonschema:"whether the calling session holds the claim"`
}

// Tasks is the shared work list every session in the manager can see and
// claim from, so a fleet coordinates without one agent brokering handoffs.
func (s *Sessions) Tasks(sessionID string) ([]Task, error) {
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return nil, err
	}
	return runtime.taskList(sessionID)
}

func (r *runtime) taskList(sessionID string) ([]Task, error) {
	stored, err := r.store.Tasks()
	if err != nil {
		return nil, err
	}
	names, err := r.sessionNames()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]store.Task, len(stored))
	for _, task := range stored {
		byID[task.ID] = task
	}
	tasks := make([]Task, 0, len(stored))
	for _, task := range stored {
		blocking := task.Blocking(byID)
		tasks = append(tasks, Task{
			ID:        task.ID,
			Title:     task.Title,
			Body:      task.Body,
			State:     task.State,
			Owner:     task.Owner,
			OwnerName: names[task.Owner],
			DependsOn: task.DependsOn,
			BlockedBy: blocking,
			Blocked:   task.State == store.TaskPending && len(blocking) > 0,
			Mine:      task.Owner == sessionID,
		})
	}
	return tasks, nil
}

func (r *runtime) sessionNames() (map[string]string, error) {
	sessions, err := r.store.ListSessions(true)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(sessions))
	for _, sess := range sessions {
		names[sess.ID] = sess.Name
	}
	return names, nil
}

func (r *runtime) task(sessionID, id string) (Task, error) {
	tasks, err := r.taskList(sessionID)
	if err != nil {
		return Task{}, err
	}
	for _, task := range tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return Task{}, fmt.Errorf("task %s does not exist; call %s for current ids", id, r.words.ListTasks)
}

func (s *Sessions) CreateTask(sessionID, title, body string, dependsOn []string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errors.New("title is empty")
	}
	runtime, err := s.open()
	if err != nil {
		return Task{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Task{}, err
	}
	for _, dep := range dependsOn {
		if _, err := runtime.store.Task(dep); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Task{}, fmt.Errorf("dependency %s does not exist; create it first, or call %s for current ids", dep, runtime.words.ListTasks)
			}
			return Task{}, err
		}
	}
	now := time.Now()
	task := store.Task{
		ID:        uuid.NewString()[:8],
		Title:     title,
		Body:      strings.TrimSpace(body),
		State:     store.TaskPending,
		DependsOn: dependsOn,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := runtime.store.CreateTask(task); err != nil {
		return Task{}, err
	}
	return runtime.task(sessionID, task.ID)
}

// ClaimTask takes a named task, or the oldest unblocked one when no id is
// given. A task another session already holds is refused rather than
// stolen: the loser of the race finds out instead of duplicating work.
func (s *Sessions) ClaimTask(sessionID, taskID string) (Task, error) {
	runtime, err := s.open()
	if err != nil {
		return Task{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Task{}, err
	}
	now := time.Now()
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		claimed, ok, err := runtime.store.ClaimNextTask(caller.ID, now)
		if err != nil {
			return Task{}, err
		}
		if !ok {
			return Task{}, errors.New("no task is ready to claim; every pending task is blocked, or the list is empty")
		}
		return runtime.task(caller.ID, claimed.ID)
	}
	claimed, err := runtime.store.ClaimTask(taskID, caller.ID, now)
	if err != nil {
		return Task{}, err
	}
	if !claimed {
		return Task{}, runtime.refusedClaim(caller.ID, taskID)
	}
	return runtime.task(caller.ID, taskID)
}

func holderOf(task Task) string {
	if task.OwnerName != "" {
		return task.OwnerName
	}
	return task.Owner
}

// refusedClaim reads the task back to say why the claim did not take,
// since "someone else got there first" and "its dependencies are not
// done" call for different next moves.
func (r *runtime) refusedClaim(sessionID, taskID string) error {
	task, err := r.task(sessionID, taskID)
	if err != nil {
		return err
	}
	switch {
	case task.State == store.TaskInProgress:
		return fmt.Errorf("task %s is already claimed by %s", taskID, holderOf(task))
	case task.State == store.TaskDone:
		return fmt.Errorf("task %s is already done", taskID)
	case task.Blocked:
		return fmt.Errorf("task %s is blocked on %s", taskID, strings.Join(task.BlockedBy, ", "))
	}
	return fmt.Errorf("task %s could not be claimed", taskID)
}

func (s *Sessions) FinishTask(sessionID, taskID string) (Task, error) {
	return s.settleTask(sessionID, taskID, true)
}

func (s *Sessions) ReleaseTask(sessionID, taskID string) (Task, error) {
	return s.settleTask(sessionID, taskID, false)
}

func (s *Sessions) settleTask(sessionID, taskID string, done bool) (Task, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, fmt.Errorf("task_id is empty; call %s to get one", s.words.ListTasks)
	}
	runtime, err := s.open()
	if err != nil {
		return Task{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Task{}, err
	}
	now := time.Now()
	settle := runtime.store.ReleaseTask
	if done {
		settle = runtime.store.FinishTask
	}
	changed, err := settle(taskID, caller.ID, now)
	if err != nil {
		return Task{}, err
	}
	if !changed {
		task, err := runtime.task(caller.ID, taskID)
		if err != nil {
			return Task{}, err
		}
		if task.State == store.TaskInProgress && task.Owner != caller.ID {
			return Task{}, fmt.Errorf("task %s is not yours to settle; %s holds it", taskID, holderOf(task))
		}
		return Task{}, fmt.Errorf("task %s is %s, not in progress", taskID, task.State)
	}
	return runtime.task(caller.ID, taskID)
}

func (s *Sessions) DeleteTask(sessionID, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("task_id is empty; call %s to get one", s.words.ListTasks)
	}
	runtime, err := s.open()
	if err != nil {
		return err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return err
	}
	deleted, err := runtime.store.DeleteTask(taskID)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("task %s does not exist; call %s for current ids", taskID, runtime.words.ListTasks)
	}
	return nil
}
