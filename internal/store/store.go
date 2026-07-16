package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Session struct {
	ID           string
	Name         string
	Tool         string
	Cwd          string
	Group        string
	Status       string
	Archived     bool
	CreatedAt    time.Time
	LastStatusAt time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id             TEXT PRIMARY KEY,
	name           TEXT NOT NULL,
	tool           TEXT NOT NULL,
	cwd            TEXT NOT NULL,
	group_name     TEXT NOT NULL,
	status         TEXT NOT NULL,
	archived       INTEGER NOT NULL DEFAULT 0,
	created_at     INTEGER NOT NULL,
	last_status_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS groups (
	name       TEXT PRIMARY KEY,
	collapsed  INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	path       TEXT NOT NULL DEFAULT ''
);`)
	if err != nil {
		return err
	}
	// Migrate older databases that predate the group default-path column.
	if _, err := s.db.Exec(`ALTER TABLE groups ADD COLUMN path TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

func (s *Store) CreateSession(sess Session) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, name, tool, cwd, group_name, status, archived, created_at, last_status_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Name, sess.Tool, sess.Cwd, sess.Group, sess.Status,
		boolToInt(sess.Archived), sess.CreatedAt.Unix(), sess.LastStatusAt.Unix(),
	)
	if err != nil {
		return err
	}
	return s.ensureGroup(sess.Group)
}

// ensureGroup registers a group by name if it does not exist, leaving any
// existing default path untouched. The empty root is never stored.
func (s *Store) ensureGroup(name string) error {
	if name == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO groups (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, name)
	return err
}

// CreateGroup registers a group path like "backend/api/auth" with an
// optional default working directory, updating the path if it already exists.
func (s *Store) CreateGroup(name, path string) error {
	if name == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO groups (name, path) VALUES (?, ?)
		 ON CONFLICT(name) DO UPDATE SET path = excluded.path`, name, path)
	return err
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := `SELECT id, name, tool, cwd, group_name, status, archived, created_at, last_status_at
	          FROM sessions`
	if !includeArchived {
		query += ` WHERE archived = 0`
	}
	query += ` ORDER BY group_name, created_at`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var archived int
		var created, lastStatus int64
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd,
			&sess.Group, &sess.Status, &archived, &created, &lastStatus); err != nil {
			return nil, err
		}
		sess.Archived = archived != 0
		sess.CreatedAt = time.Unix(created, 0)
		sess.LastStatusAt = time.Unix(lastStatus, 0)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Get(id string) (Session, error) {
	var sess Session
	var archived int
	var created, lastStatus int64
	err := s.db.QueryRow(
		`SELECT id, name, tool, cwd, group_name, status, archived, created_at, last_status_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd, &sess.Group,
		&sess.Status, &archived, &created, &lastStatus)
	if err != nil {
		return Session{}, err
	}
	sess.Archived = archived != 0
	sess.CreatedAt = time.Unix(created, 0)
	sess.LastStatusAt = time.Unix(lastStatus, 0)
	return sess, nil
}

func (s *Store) UpdateStatus(id, newStatus string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, last_status_at = ? WHERE id = ?`,
		newStatus, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

func (s *Store) SetArchived(id string, archived bool) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET archived = ? WHERE id = ?`, boolToInt(archived), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SessionsInSubtree returns every session (archived included) whose group
// is the given path or any descendant of it.
func (s *Store) SessionsInSubtree(path string) ([]Session, error) {
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	var matched []Session
	for _, sess := range sessions {
		if sess.Group == path || strings.HasPrefix(sess.Group, path+"/") {
			matched = append(matched, sess)
		}
	}
	return matched, nil
}

// RenameGroup rewrites a group path and every descendant group and
// session under it. Fails if the destination path already exists.
func (s *Store) RenameGroup(oldPath, newPath string) error {
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("group path cannot be empty")
	}
	if oldPath == newPath {
		return nil
	}
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM groups WHERE name = ?)`, newPath).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 1 {
		return fmt.Errorf("group %s already exists", newPath)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		`UPDATE groups SET name = ? || substr(name, length(?)+1)
		 WHERE name = ? OR name LIKE ? || '/%'`,
		newPath, oldPath, oldPath, oldPath)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`UPDATE sessions SET group_name = ? || substr(group_name, length(?)+1)
		 WHERE group_name = ? OR group_name LIKE ? || '/%'`,
		newPath, oldPath, oldPath, oldPath)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MoveSession reassigns a session to another group ("" = root).
func (s *Store) MoveSession(id, group string) error {
	res, err := s.db.Exec(`UPDATE sessions SET group_name = ? WHERE id = ?`, group, id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	return s.ensureGroup(group)
}

// RenameSession changes a session's display name.
func (s *Store) RenameSession(id, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("session name cannot be empty")
	}
	res, err := s.db.Exec(`UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// DeleteGroup removes a group and all its descendant groups.
func (s *Store) DeleteGroup(path string) error {
	if path == "" {
		return fmt.Errorf("cannot delete the root group")
	}
	_, err := s.db.Exec(
		`DELETE FROM groups WHERE name = ? OR name LIKE ? || '/%'`, path, path)
	return err
}

type Group struct {
	Name string
	Path string
}

func (s *Store) Groups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT name, path FROM groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Path); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func requireRow(res sql.Result, id string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
