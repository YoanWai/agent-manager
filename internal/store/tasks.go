package store

import (
	"database/sql"
	"errors"
	"time"
)

const (
	TaskPending    = "pending"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
)

// Task is one unit of shared work agents claim for themselves. The list
// is what lets a fleet coordinate without a lead agent brokering every
// handoff.
type Task struct {
	ID        string
	Title     string
	Body      string
	Owner     string
	State     string
	DependsOn []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Blocking lists the dependencies that are not done, which is the part of
// depends_on worth telling anyone about: a finished dependency is in
// nobody's way. It is only meaningful alongside the tasks it names.
func (t Task) Blocking(byID map[string]Task) []string {
	blocking := make([]string, 0, len(t.DependsOn))
	for _, id := range t.DependsOn {
		if dep, ok := byID[id]; !ok || dep.State != TaskDone {
			blocking = append(blocking, id)
		}
	}
	return blocking
}

func (s *Store) CreateTask(task Task) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO tasks (id, title, body, owner_session_id, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Title, task.Body, task.Owner, task.State,
		encodeTime(task.CreatedAt), encodeTime(task.UpdatedAt)); err != nil {
		return err
	}
	for _, dep := range task.DependsOn {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO task_deps (task_id, depends_on_id) VALUES (?, ?)`,
			task.ID, dep); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClaimTask takes a named task, refusing one already claimed or still
// blocked. SQLite serializes writers, so the guard and the write happen
// inside one write transaction and a second agent racing for the same
// task matches zero rows rather than overwriting the winner.
func (s *Store) ClaimTask(id, sessionID string, at time.Time) (bool, error) {
	res, err := s.db.Exec(`
UPDATE tasks SET state = ?, owner_session_id = ?, updated_at = ?
 WHERE id = ? AND state = ?
   AND NOT EXISTS (
     SELECT 1 FROM task_deps d JOIN tasks p ON p.id = d.depends_on_id
      WHERE d.task_id = tasks.id AND p.state != ?
   )`, TaskInProgress, sessionID, encodeTime(at), id, TaskPending, TaskDone)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// ClaimNextTask takes the oldest task that is pending and unblocked.
func (s *Store) ClaimNextTask(sessionID string, at time.Time) (Task, bool, error) {
	task := Task{Owner: sessionID, State: TaskInProgress, UpdatedAt: at}
	var createdAt int64
	err := s.db.QueryRow(`
UPDATE tasks SET state = ?, owner_session_id = ?, updated_at = ?
 WHERE id = (
   SELECT t.id FROM tasks t
    WHERE t.state = ?
      AND NOT EXISTS (
        SELECT 1 FROM task_deps d JOIN tasks p ON p.id = d.depends_on_id
         WHERE d.task_id = t.id AND p.state != ?
      )
    ORDER BY t.created_at, t.id LIMIT 1
 )
 AND state = ?
 RETURNING id, title, body, created_at`,
		TaskInProgress, sessionID, encodeTime(at), TaskPending, TaskDone, TaskPending).
		Scan(&task.ID, &task.Title, &task.Body, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	task.CreatedAt = decodeTime(createdAt)
	return task, true, nil
}

// FinishTask marks a claim done, which unblocks whatever depended on it.
func (s *Store) FinishTask(id, sessionID string, at time.Time) (bool, error) {
	return s.settleTask(id, sessionID, TaskDone, sessionID, at)
}

// ReleaseTask hands a claim back, so another agent can pick the work up.
func (s *Store) ReleaseTask(id, sessionID string, at time.Time) (bool, error) {
	return s.settleTask(id, sessionID, TaskPending, "", at)
}

func (s *Store) settleTask(id, sessionID, state, owner string, at time.Time) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE tasks SET state = ?, owner_session_id = ?, updated_at = ?
		  WHERE id = ? AND owner_session_id = ? AND state = ?`,
		state, owner, encodeTime(at), id, sessionID, TaskInProgress)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (s *Store) DeleteTask(id string) (bool, error) {
	if _, err := s.db.Exec(`DELETE FROM task_deps WHERE task_id = ? OR depends_on_id = ?`, id, id); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (s *Store) Tasks() ([]Task, error) {
	rows, err := s.db.Query(`
SELECT id, title, body, owner_session_id, state, created_at, updated_at
  FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]Task, 0)
	index := map[string]int{}
	for rows.Next() {
		var task Task
		var createdAt, updatedAt int64
		if err := rows.Scan(&task.ID, &task.Title, &task.Body, &task.Owner, &task.State, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		task.CreatedAt = decodeTime(createdAt)
		task.UpdatedAt = decodeTime(updatedAt)
		index[task.ID] = len(tasks)
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, s.loadTaskDeps(tasks, index)
}

func (s *Store) loadTaskDeps(tasks []Task, index map[string]int) error {
	if len(tasks) == 0 {
		return nil
	}
	rows, err := s.db.Query(`SELECT task_id, depends_on_id FROM task_deps`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, dep string
		if err := rows.Scan(&id, &dep); err != nil {
			return err
		}
		if at, ok := index[id]; ok {
			tasks[at].DependsOn = append(tasks[at].DependsOn, dep)
		}
	}
	return rows.Err()
}

func (s *Store) Task(id string) (Task, error) {
	tasks, err := s.Tasks()
	if err != nil {
		return Task{}, err
	}
	for _, task := range tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return Task{}, sql.ErrNoRows
}

// ReleaseTasksOwnedBy hands back every claim a session held, so work does
// not sit in progress forever behind an agent that is gone.
func (s *Store) ReleaseTasksOwnedBy(sessionID string, at time.Time) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET state = ?, owner_session_id = '', updated_at = ?
		  WHERE owner_session_id = ? AND state = ?`,
		TaskPending, encodeTime(at), sessionID, TaskInProgress)
	return err
}
