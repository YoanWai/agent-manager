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
	Acked        bool
	CreatedAt    time.Time
	LastStatusAt time.Time
	// AgentSessionID is the agent CLI's own conversation id (claude/grok
	// session UUID, codex rollout id, opencode session id). Revive resumes
	// this exact conversation instead of the cwd's most recent one.
	AgentSessionID string
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
	last_status_at INTEGER NOT NULL,
	agent_session_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS groups (
	name       TEXT PRIMARY KEY,
	sort_order INTEGER NOT NULL DEFAULT 0,
	path       TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	// Migrate older databases that predate the group default-path column
	// and the session sort-order column.
	migrations := []string{
		`ALTER TABLE groups ADD COLUMN path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN acked INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN agent_session_id TEXT NOT NULL DEFAULT ''`,
	}
	for _, migration := range migrations {
		if _, err := s.db.Exec(migration); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Setting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) CreateSession(sess Session) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, name, tool, cwd, group_name, status, archived, created_at, last_status_at, agent_session_id, sort_order)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		         (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ?))`,
		sess.ID, sess.Name, sess.Tool, sess.Cwd, sess.Group, sess.Status,
		boolToInt(sess.Archived), encodeTime(sess.CreatedAt), encodeTime(sess.LastStatusAt), sess.AgentSessionID, sess.Group,
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
		`INSERT INTO groups (name, sort_order)
		 VALUES (?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
		 ON CONFLICT(name) DO NOTHING`, name)
	return err
}

// CreateGroup registers a group path like "backend/api/auth" with an
// optional default working directory, updating the path if it already exists.
func (s *Store) CreateGroup(name, path string) error {
	if name == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO groups (name, path, sort_order)
		 VALUES (?, ?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
		 ON CONFLICT(name) DO UPDATE SET path = excluded.path`, name, path)
	return err
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := `SELECT id, name, tool, cwd, group_name, status, archived, acked, created_at, last_status_at, agent_session_id
	          FROM sessions`
	if !includeArchived {
		query += ` WHERE archived = 0`
	}
	query += ` ORDER BY group_name, sort_order, created_at`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var archived, acked int
		var created, lastStatus int64
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd,
			&sess.Group, &sess.Status, &archived, &acked, &created, &lastStatus,
			&sess.AgentSessionID); err != nil {
			return nil, err
		}
		sess.Archived = archived != 0
		sess.Acked = acked != 0
		sess.CreatedAt = decodeTime(created)
		sess.LastStatusAt = decodeTime(lastStatus)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Get(id string) (Session, error) {
	var sess Session
	var archived, acked int
	var created, lastStatus int64
	err := s.db.QueryRow(
		`SELECT id, name, tool, cwd, group_name, status, archived, acked, created_at, last_status_at, agent_session_id
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd, &sess.Group,
		&sess.Status, &archived, &acked, &created, &lastStatus, &sess.AgentSessionID)
	if err != nil {
		return Session{}, err
	}
	sess.Archived = archived != 0
	sess.Acked = acked != 0
	sess.CreatedAt = decodeTime(created)
	sess.LastStatusAt = decodeTime(lastStatus)
	return sess, nil
}

func (s *Store) UpdateStatus(id, newStatus string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, last_status_at = ? WHERE id = ?`,
		newStatus, encodeTime(time.Now()), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetAcked marks whether the user has acknowledged the session's last
// finished turn; an acked session renders idle even while its pane still
// shows the finished turn.
func (s *Store) SetAcked(id string, acked bool) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET acked = ? WHERE id = ?`, boolToInt(acked), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetAgentSessionID records the agent CLI's own conversation id for a
// session, so a later revive resumes that exact conversation. Used both
// when launching a tool we assign the id to and when capturing the id a
// tool minted itself.
func (s *Store) SetAgentSessionID(id, agentSessionID string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET agent_session_id = ? WHERE id = ?`, agentSessionID, id)
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

// MoveSession reassigns a session to another group ("" = root), placing
// it at the end of the destination.
func (s *Store) MoveSession(id, group string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET group_name = ?,
		 sort_order = (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ?)
		 WHERE id = ?`, group, group, id)
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

// ReorderSession moves a session one visible step among its group
// siblings, reporting whether anything moved. Hidden (archived)
// siblings are skipped when the caller's view excludes them, so a move
// always has a visible effect. Siblings are renumbered to a dense 0..n
// first, since fresh databases start with ties.
func (s *Store) ReorderSession(id string, delta int, includeArchived bool) (bool, error) {
	sess, err := s.Get(id)
	if err != nil {
		return false, err
	}
	rows, err := s.db.Query(
		`SELECT id, archived FROM sessions WHERE group_name = ?
		 ORDER BY sort_order, created_at`, sess.Group)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	type sibling struct {
		id       string
		archived bool
	}
	var siblings []sibling
	for rows.Next() {
		var sib sibling
		var archived int
		if err := rows.Scan(&sib.id, &archived); err != nil {
			return false, err
		}
		sib.archived = archived != 0
		siblings = append(siblings, sib)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	current, target := -1, -1
	for i, sib := range siblings {
		if sib.id == id {
			current = i
			break
		}
	}
	if current < 0 {
		return false, fmt.Errorf("session %s not found among its siblings", id)
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := current + step; i >= 0 && i < len(siblings); i += step {
		if siblings[i].archived && !includeArchived {
			continue
		}
		target = i
		break
	}
	if target < 0 {
		return false, nil
	}

	siblings[current], siblings[target] = siblings[target], siblings[current]
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	for i, sib := range siblings {
		if _, err := tx.Exec(`UPDATE sessions SET sort_order = ? WHERE id = ?`, i, sib.id); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ReorderGroup moves a group one step among the groups sharing its
// parent path, reporting whether anything moved. All groups are
// renumbered to their current global order first so sibling swaps are
// well-defined.
func (s *Store) ReorderGroup(path string, delta int) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("cannot reorder the root group")
	}
	if err := s.ensureGroup(path); err != nil {
		return false, err
	}
	groups, err := s.Groups()
	if err != nil {
		return false, err
	}
	parent := ""
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		parent = path[:idx]
	}
	isSibling := func(name string) bool {
		if parent == "" {
			return !strings.Contains(name, "/")
		}
		rest, ok := strings.CutPrefix(name, parent+"/")
		return ok && !strings.Contains(rest, "/")
	}

	current, target := -1, -1
	for i, g := range groups {
		if g.Name == path {
			current = i
			break
		}
	}
	if current < 0 {
		return false, fmt.Errorf("group %s not found", path)
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := current + step; i >= 0 && i < len(groups); i += step {
		if isSibling(groups[i].Name) {
			target = i
			break
		}
	}
	if target < 0 {
		return false, nil
	}

	groups[current], groups[target] = groups[target], groups[current]
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	for i, g := range groups {
		if _, err := tx.Exec(`UPDATE groups SET sort_order = ? WHERE name = ?`, i, g.Name); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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

// secondsCeiling separates the two timestamp encodings the sessions table
// has held. Values below it are Unix seconds written before nanosecond
// precision (a seconds timestamp stays under it until the year 33658); at
// or above it are Unix nanoseconds (any real nanosecond timestamp since
// 1970 far exceeds it). This lets decodeTime read old rows without a data
// migration.
const secondsCeiling int64 = 1e12

// encodeTime stores a timestamp as Unix nanoseconds so sessions launched in
// the same second keep a distinct, ordered launch time. The zero time
// encodes as 0.
func encodeTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// decodeTime reverses encodeTime, reading pre-precision rows as seconds.
func decodeTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	if v < secondsCeiling {
		return time.Unix(v, 0)
	}
	return time.Unix(0, v)
}
