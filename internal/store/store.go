package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	_ "modernc.org/sqlite"
)

// ErrSessionGone reports a write against a session row that is no longer
// there. Deleting a session is normal, so a caller holding a session
// listed a moment earlier can tell that race apart from a real failure.
var ErrSessionGone = errors.New("session no longer exists")

var ErrGroupExists = errors.New("group already exists")

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
	// AgentSessionID is the agent CLI's own conversation id (claude/grok/
	// gemini/pi session UUID, codex rollout id, opencode/hermes session id).
	// Revive resumes this exact conversation instead of the cwd's most recent one.
	AgentSessionID string
	// AgentLaunchedAt is when the agent process now in the pane started, which
	// restart and revive move forward while CreatedAt keeps marking the row's birth.
	// Zero for sessions that never relaunched, whose launch is CreatedAt.
	AgentLaunchedAt time.Time
	// RetiredAgentSessionID is the conversation a restart left behind, kept so
	// id capture never binds the fresh run back to the context it dropped.
	RetiredAgentSessionID string
	// RelaunchSnapshot is the tool's store state taken right before the
	// relaunch pane started: each conversation the directory held with its
	// last activity time. Recapture binds only conversations whose activity
	// outruns this, which tells the conversation the picker selected apart
	// from ones that merely predate the launch.
	RelaunchSnapshot map[string]int64
	// WorktreeRepo and WorktreeBranch are set for sessions running in a
	// worktree Agent Manager created. Forks share these values so the last
	// session to leave can clean up the worktree and its am/ branch.
	WorktreeRepo        string
	WorktreeBranch      string
	PendingInputs       []string
	PendingInputClaimed bool
	ParentID            string
	// LaunchPrompt is the prompt handed to the agent on its command line.
	// Pending input waits for it to show in the pane, because an agent
	// taking it clears the composer and anything pasted there.
	LaunchPrompt string
	// LastPrompt is the most recent text delivered to the session through
	// the manager: a quick bar send, a launch input the poller typed in, or
	// a queued message from another agent. The full screen row wears it,
	// falling back to LaunchPrompt for sessions nothing was sent to since.
	LastPrompt string
	// TmuxSocket is the tmux server the session's pane runs on. A manager
	// only derives status for the sessions on its own server: a pane it
	// cannot see belongs to another manager, not to a dead agent.
	TmuxSocket string
}

// LaunchTime is when the agent now in the pane started: the last restart
// or revive, or the row's creation for a session that never relaunched.
func (sess Session) LaunchTime() time.Time {
	if sess.AgentLaunchedAt.IsZero() {
		return sess.CreatedAt
	}
	return sess.AgentLaunchedAt
}

type Store struct {
	db *sql.DB
}

// busyTimeout is how long a writer waits for the lock before giving up.
// The manager and every `agent-manager mcp` process share this database,
// so without it a collision between the poller's write and a session
// tool's write fails instantly with SQLITE_BUSY instead of waiting.
// _txlock=immediate takes the write lock at BEGIN, so a multi-statement
// transaction cannot fail halfway through trying to upgrade.
const busyTimeout = "?_pragma=busy_timeout(5000)&_txlock=immediate"

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+busyTimeout)
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
	agent_session_id TEXT NOT NULL DEFAULT '',
	pending_inputs TEXT NOT NULL DEFAULT '[]',
	pending_claimed INTEGER NOT NULL DEFAULT 0,
	launch_prompt  TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS groups (
	name       TEXT PRIMARY KEY,
	sort_order INTEGER NOT NULL DEFAULT 0,
	path       TEXT NOT NULL DEFAULT '',
	archived   INTEGER NOT NULL DEFAULT 0
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
		`ALTER TABLE groups ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN snapshot TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS review_targets (
			session_id TEXT PRIMARY KEY,
			repo_root  TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS review_bases (
			session_id TEXT NOT NULL,
			repo_root  TEXT NOT NULL,
			base_ref   TEXT NOT NULL,
			PRIMARY KEY (session_id, repo_root)
		)`,
		`CREATE TABLE IF NOT EXISTS review_scopes (
			session_id TEXT PRIMARY KEY,
			scope      TEXT NOT NULL
		)`,
		`ALTER TABLE sessions ADD COLUMN worktree_repo TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN worktree_branch TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE groups ADD COLUMN worktree TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN agent_launched_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN retired_agent_session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN pending_inputs TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE sessions ADD COLUMN pending_claimed INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN launch_prompt TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS session_inbox (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT    NOT NULL,
			sender_id    TEXT    NOT NULL,
			sender_name  TEXT    NOT NULL,
			body         TEXT    NOT NULL,
			fingerprint  TEXT    NOT NULL,
			sent_at      INTEGER NOT NULL,
			claimed_at   INTEGER NOT NULL DEFAULT 0,
			delivered_at INTEGER NOT NULL DEFAULT 0,
			read_at      INTEGER NOT NULL DEFAULT 0
		)`,
		`ALTER TABLE session_inbox ADD COLUMN dropped_at INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS session_inbox_queue ON session_inbox (session_id, delivered_at, id)`,
		`CREATE INDEX IF NOT EXISTS session_inbox_sender ON session_inbox (session_id, sender_id, sent_at)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id               TEXT PRIMARY KEY,
			title            TEXT NOT NULL,
			body             TEXT NOT NULL DEFAULT '',
			owner_session_id TEXT NOT NULL DEFAULT '',
			state            TEXT NOT NULL DEFAULT 'pending',
			created_at       INTEGER NOT NULL DEFAULT 0,
			updated_at       INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS tasks_claimable ON tasks (state, created_at)`,
		`CREATE TABLE IF NOT EXISTS task_deps (
			task_id       TEXT NOT NULL,
			depends_on_id TEXT NOT NULL,
			PRIMARY KEY (task_id, depends_on_id)
		)`,
		`CREATE INDEX IF NOT EXISTS task_deps_reverse ON task_deps (depends_on_id)`,
		`CREATE TABLE IF NOT EXISTS file_reservations (
			id          TEXT    PRIMARY KEY,
			session_id  TEXT    NOT NULL,
			pattern     TEXT    NOT NULL,
			mode        TEXT    NOT NULL DEFAULT 'exclusive',
			note        TEXT    NOT NULL DEFAULT '',
			acquired_at INTEGER NOT NULL DEFAULT 0,
			expires_at  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS file_reservations_holder ON file_reservations (session_id, pattern)`,
		`CREATE INDEX IF NOT EXISTS file_reservations_live ON file_reservations (expires_at)`,
		`CREATE TABLE IF NOT EXISTS review_states (
			session_id TEXT NOT NULL,
			repo_root  TEXT NOT NULL,
			state      TEXT NOT NULL,
			PRIMARY KEY (session_id, repo_root)
		)`,
		`ALTER TABLE sessions ADD COLUMN tmux_socket TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN last_prompt TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN relaunch_snapshot TEXT NOT NULL DEFAULT ''`,
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

func (s *Store) SetReviewRepo(sessionID, repoRoot string) error {
	if repoRoot == "" {
		_, err := s.db.Exec(`DELETE FROM review_targets WHERE session_id = ?`, sessionID)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_targets (session_id, repo_root) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET repo_root = excluded.repo_root`,
		sessionID, repoRoot,
	)
	return err
}

func (s *Store) ReviewRepo(sessionID string) (string, error) {
	var root string
	err := s.db.QueryRow(`SELECT repo_root FROM review_targets WHERE session_id = ?`, sessionID).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return root, nil
}

func (s *Store) SetReviewBase(sessionID, repoRoot, baseRef string) error {
	if baseRef == "" {
		_, err := s.db.Exec(
			`DELETE FROM review_bases WHERE session_id = ? AND repo_root = ?`,
			sessionID, repoRoot)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_bases (session_id, repo_root, base_ref) VALUES (?, ?, ?)
		 ON CONFLICT(session_id, repo_root) DO UPDATE SET base_ref = excluded.base_ref`,
		sessionID, repoRoot, baseRef,
	)
	return err
}

func (s *Store) ReviewBase(sessionID, repoRoot string) (string, error) {
	var ref string
	err := s.db.QueryRow(
		`SELECT base_ref FROM review_bases WHERE session_id = ? AND repo_root = ?`,
		sessionID, repoRoot).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ref, nil
}

func (s *Store) SetReviewScope(sessionID, scope string) error {
	if scope == "" {
		_, err := s.db.Exec(`DELETE FROM review_scopes WHERE session_id = ?`, sessionID)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_scopes (session_id, scope) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET scope = excluded.scope`,
		sessionID, scope,
	)
	return err
}

func (s *Store) ReviewScope(sessionID string) (string, error) {
	var scope string
	err := s.db.QueryRow(`SELECT scope FROM review_scopes WHERE session_id = ?`, sessionID).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return scope, nil
}

// paneSizeSetting carries the window size the running manager pins its
// session panes to. The CLI and the MCP server open panes with no manager
// to ask, and a pane born at tmux's own 80x24 default stays there until
// something resizes it, so they read the box from here instead.
const paneSizeSetting = "pane_size"

func (s *Store) SetPaneSize(width, height int) error {
	return s.SetSetting(paneSizeSetting, fmt.Sprintf("%dx%d", width, height))
}

// PaneSize is the box the manager last pinned panes to, zeroes when no
// manager has recorded one yet, which leaves the size to tmux.
func (s *Store) PaneSize() (int, int, error) {
	value, err := s.Setting(paneSizeSetting)
	if err != nil || value == "" {
		return 0, 0, err
	}
	columns, rows, ok := strings.Cut(value, "x")
	width, widthErr := strconv.Atoi(columns)
	height, heightErr := strconv.Atoi(rows)
	if !ok || widthErr != nil || heightErr != nil {
		return 0, 0, fmt.Errorf("pane size %q in settings is not <columns>x<rows>", value)
	}
	return width, height, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) CreateSession(sess Session) error {
	return s.createSession(sess, "")
}

// CreateSessionBeside reads the anchor inside the write transaction, so a
// placement that lands first decides where the new row goes rather than
// leaving it behind.
func (s *Store) CreateSessionBeside(sess Session, anchorID string) error {
	return s.createSession(sess, anchorID)
}

func (s *Store) createSession(sess Session, anchorID string) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	pendingInputs, err := encodePendingInputs(sess.PendingInputs)
	if err != nil {
		return err
	}
	sess.ParentID = strings.TrimSpace(sess.ParentID)
	anchorID = strings.TrimSpace(anchorID)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if anchorID != "" {
		var anchorGroup, anchorParent string
		err := tx.QueryRow(`SELECT group_name, parent_id FROM sessions WHERE id = ?`, anchorID).Scan(&anchorGroup, &anchorParent)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("session %s: %w", anchorID, err)
		}
		if err != nil {
			return err
		}
		sess.ParentID = anchorParent
		sess.Group = anchorGroup
	}
	if sess.ParentID != "" {
		parentGroup, err := validParent(tx, sess.ID, sess.ParentID)
		if err != nil {
			return err
		}
		sess.Group = parentGroup
	}
	_, err = tx.Exec(
		`INSERT INTO sessions (id, name, tool, cwd, group_name, status, archived, created_at, last_status_at, agent_session_id, worktree_repo, worktree_branch, pending_inputs, parent_id, launch_prompt, tmux_socket, sort_order)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		         (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ? AND parent_id = ?))`,
		sess.ID, sess.Name, sess.Tool, sess.Cwd, sess.Group, sess.Status,
		boolToInt(sess.Archived), encodeTime(sess.CreatedAt), encodeTime(sess.LastStatusAt), sess.AgentSessionID,
		sess.WorktreeRepo, sess.WorktreeBranch, pendingInputs, sess.ParentID, sess.LaunchPrompt, sess.TmuxSocket,
		sess.Group, sess.ParentID,
	)
	if err != nil {
		return err
	}
	if sess.Group != "" {
		if _, err := tx.Exec(
			`INSERT INTO groups (name, sort_order)
			 VALUES (?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
			 ON CONFLICT(name) DO NOTHING`, sess.Group); err != nil {
			return err
		}
	}
	return tx.Commit()
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

func (s *Store) AddGroup(name, path, worktree string) error {
	if name == "" {
		return errors.New("group name cannot be empty")
	}
	res, err := s.db.Exec(
		`INSERT INTO groups (name, path, worktree, sort_order)
		 VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
		 ON CONFLICT(name) DO NOTHING`, name, path, worktree)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("group %q: %w", name, ErrGroupExists)
	}
	return nil
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := `SELECT id, name, tool, cwd, group_name, status, archived, acked, created_at, last_status_at, agent_session_id, worktree_repo, worktree_branch, agent_launched_at, retired_agent_session_id, relaunch_snapshot, pending_inputs, pending_claimed, parent_id, launch_prompt, last_prompt, tmux_socket
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
		var archived, acked, pendingClaimed int
		var created, lastStatus, agentLaunched int64
		var pendingInputs, relaunchSnapshot string
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd,
			&sess.Group, &sess.Status, &archived, &acked, &created, &lastStatus,
			&sess.AgentSessionID, &sess.WorktreeRepo, &sess.WorktreeBranch,
			&agentLaunched, &sess.RetiredAgentSessionID, &relaunchSnapshot, &pendingInputs, &pendingClaimed, &sess.ParentID, &sess.LaunchPrompt, &sess.LastPrompt, &sess.TmuxSocket); err != nil {
			return nil, err
		}
		if err := decodeRelaunchSnapshot(relaunchSnapshot, &sess); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(pendingInputs), &sess.PendingInputs); err != nil {
			return nil, fmt.Errorf("decode pending inputs for session %s: %w", sess.ID, err)
		}
		sess.Archived = archived != 0
		sess.Acked = acked != 0
		sess.PendingInputClaimed = pendingClaimed != 0
		sess.CreatedAt = decodeTime(created)
		sess.LastStatusAt = decodeTime(lastStatus)
		sess.AgentLaunchedAt = decodeTime(agentLaunched)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Get(id string) (Session, error) {
	var sess Session
	var archived, acked, pendingClaimed int
	var created, lastStatus, agentLaunched int64
	var pendingInputs, relaunchSnapshot string
	err := s.db.QueryRow(
		`SELECT id, name, tool, cwd, group_name, status, archived, acked, created_at, last_status_at, agent_session_id, worktree_repo, worktree_branch, agent_launched_at, retired_agent_session_id, relaunch_snapshot, pending_inputs, pending_claimed, parent_id, launch_prompt, last_prompt, tmux_socket
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd, &sess.Group,
		&sess.Status, &archived, &acked, &created, &lastStatus, &sess.AgentSessionID,
		&sess.WorktreeRepo, &sess.WorktreeBranch, &agentLaunched, &sess.RetiredAgentSessionID, &relaunchSnapshot, &pendingInputs, &pendingClaimed, &sess.ParentID, &sess.LaunchPrompt, &sess.LastPrompt, &sess.TmuxSocket)
	if err != nil {
		return Session{}, err
	}
	if err := decodeRelaunchSnapshot(relaunchSnapshot, &sess); err != nil {
		return Session{}, err
	}
	if err := json.Unmarshal([]byte(pendingInputs), &sess.PendingInputs); err != nil {
		return Session{}, fmt.Errorf("decode pending inputs for session %s: %w", sess.ID, err)
	}
	sess.Archived = archived != 0
	sess.Acked = acked != 0
	sess.PendingInputClaimed = pendingClaimed != 0
	sess.CreatedAt = decodeTime(created)
	sess.LastStatusAt = decodeTime(lastStatus)
	sess.AgentLaunchedAt = decodeTime(agentLaunched)
	return sess, nil
}

func (s *Store) Children(parentID string) ([]Session, error) {
	if parentID == "" {
		return nil, nil
	}
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	var kids []Session
	for _, sess := range sessions {
		if sess.ParentID == parentID {
			kids = append(kids, sess)
		}
	}
	return kids, nil
}

func (s *Store) ClaimPendingInput(id, expected string) (bool, error) {
	encoded, inputs, claimed, err := s.pendingInputState(id)
	if err != nil {
		return false, err
	}
	if claimed || len(inputs) == 0 || inputs[0] != expected {
		return false, nil
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_claimed = 1
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 0`, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, s.requireRowOrNoop(res, id)
	}
	return true, nil
}

func (s *Store) ConsumeClaimedPendingInput(id, expected string) (bool, error) {
	encoded, inputs, claimed, err := s.pendingInputState(id)
	if err != nil {
		return false, err
	}
	if !claimed || len(inputs) == 0 || inputs[0] != expected {
		return false, nil
	}
	remaining, err := encodePendingInputs(inputs[1:])
	if err != nil {
		return false, err
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_inputs = ?, pending_claimed = 0
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1`,
		remaining, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, s.requireRowOrNoop(res, id)
	}
	return true, nil
}

func (s *Store) pendingInputState(id string) (string, []string, bool, error) {
	var encoded string
	var claimed int
	if err := s.db.QueryRow(
		`SELECT pending_inputs, pending_claimed FROM sessions WHERE id = ?`, id,
	).Scan(&encoded, &claimed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, false, fmt.Errorf("session %s: %w", id, ErrSessionGone)
		}
		return "", nil, false, err
	}
	var inputs []string
	if err := json.Unmarshal([]byte(encoded), &inputs); err != nil {
		return "", nil, false, fmt.Errorf("decode pending inputs for session %s: %w", id, err)
	}
	return encoded, inputs, claimed != 0, nil
}

func encodePendingInputs(inputs []string) (string, error) {
	if len(inputs) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(inputs)
	return string(encoded), err
}

// decodeRelaunchSnapshot fills the session's snapshot map from the stored
// JSON. An empty column means the relaunch predates snapshot capture, which
// recapture treats as "nothing known" and refuses to guess from.
func decodeRelaunchSnapshot(encoded string, sess *Session) error {
	if encoded == "" {
		sess.RelaunchSnapshot = nil
		return nil
	}
	if err := json.Unmarshal([]byte(encoded), &sess.RelaunchSnapshot); err != nil {
		return fmt.Errorf("decode relaunch snapshot for session %s: %w", sess.ID, err)
	}
	return nil
}

// SetRelaunchSnapshot stores the tool-store state taken right before a
// relaunch, and clears it once the conversation has bound or the launch is
// over. Written before the pane starts, so it never races the launch itself.
func (s *Store) SetRelaunchSnapshot(id string, snapshot map[string]int64) error {
	encoded := ""
	if snapshot != nil {
		// An empty non-nil map is a real pre-launch state ("the store held
		// nothing"), so it persists as {} where nil clears the column.
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		encoded = string(data)
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET relaunch_snapshot = ? WHERE id = ?`, encoded, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
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

// UpdateStatusOnSocket writes a status only while the row is still this
// manager's to speak for: claimed by this tmux server, or claimed by none.
// It reports whether the write landed, so a manager whose listing predates
// another one claiming the row leaves that row's status alone rather than
// announcing what it derived from a pane it cannot see.
func (s *Store) UpdateStatusOnSocket(id, newStatus, socket string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, last_status_at = ?
		 WHERE id = ? AND tmux_socket IN ('', ?)`,
		newStatus, encodeTime(time.Now()), id, socket)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed > 0, nil
}

// AcknowledgeFinished atomically marks a session idle and acked if its stored
// status is still finished. A newer status makes the operation a no-op.
func (s *Store) AcknowledgeFinished(id string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, acked = 1, last_status_at = ? WHERE id = ? AND status = ?`,
		status.Idle, encodeTime(time.Now()), id, status.Finished)
	if err != nil {
		return err
	}
	return s.requireRowOrNoop(res, id)
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

// SetLastPrompt records the text a delivery just put in front of the
// session, which the full screen row shows beside its name.
func (s *Store) SetLastPrompt(id, prompt string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET last_prompt = ? WHERE id = ?`, prompt, id)
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

// BindAgentSessionID records a captured conversation id, but only while the
// session is still the launch the capture ran for: unbound, and launched at
// the moment the capturing pass read. It reports whether the write landed.
// Capture reads a tool's store from a snapshot and can take minutes, long
// enough for a restart to clear the id and move the launch on underneath it,
// and that stale answer names the conversation the restart just dropped.
func (s *Store) BindAgentSessionID(id, agentSessionID string, launchedAt time.Time) (bool, error) {
	// The snapshot clears in the same statement as the bind, so a relaunch
	// landing between the two can never have its fresh snapshot wiped by a
	// bind that answered the previous launch.
	res, err := s.db.Exec(
		`UPDATE sessions SET agent_session_id = ?, relaunch_snapshot = ''
		 WHERE id = ? AND agent_session_id = '' AND agent_launched_at = ?`,
		agentSessionID, id, encodeTime(launchedAt))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RestartAgent rebinds a session to a fresh agent run: the conversation it
// was resuming is retired, the new one (empty until capture for tools that
// mint their own id) takes its place, and the launch clock moves to now.
func (s *Store) RestartAgent(id, agentSessionID string, launchedAt time.Time) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET
			retired_agent_session_id = CASE WHEN agent_session_id != '' THEN agent_session_id ELSE retired_agent_session_id END,
			agent_session_id = ?,
			agent_launched_at = ?
		 WHERE id = ?`, agentSessionID, encodeTime(launchedAt), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetAgentLaunchedAt records when the agent now in the pane started, without
// touching the conversation it is resuming.
func (s *Store) SetAgentLaunchedAt(id string, launchedAt time.Time) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET agent_launched_at = ? WHERE id = ?`,
		encodeTime(launchedAt), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetTmuxSocket records the tmux server a session's pane was found on,
// which is how a later manager tells a session it cannot drive from one
// whose agent has died.
func (s *Store) SetTmuxSocket(id, socket string) error {
	res, err := s.db.Exec(`UPDATE sessions SET tmux_socket = ? WHERE id = ?`, socket, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetSnapshot stores the session's final pane capture, kept out of the
// Session struct so list queries never haul the blob.
func (s *Store) SetSnapshot(id, snapshot string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET snapshot = ? WHERE id = ?`, snapshot, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

func (s *Store) Snapshot(id string) (string, error) {
	var snapshot string
	err := s.db.QueryRow(`SELECT snapshot FROM sessions WHERE id = ?`, id).Scan(&snapshot)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return snapshot, err
}

func (s *Store) SetArchived(id string, archived bool) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET archived = ? WHERE id = ?`, boolToInt(archived), id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	// Restoring a session out of an archived group must leave it with a live
	// home, so un-archive its group and every ancestor.
	if !archived {
		sess, err := s.Get(id)
		if err != nil {
			return err
		}
		return s.unarchiveAncestorGroups(sess.Group)
	}
	return nil
}

// unarchiveAncestorGroups clears the archived flag on a group path and each
// of its ancestors, leaving descendants untouched.
func (s *Store) unarchiveAncestorGroups(path string) error {
	var err error
	eachAncestor(path, func(ancestor string) bool {
		_, err = s.db.Exec(`UPDATE groups SET archived = 0 WHERE name = ?`, ancestor)
		return err == nil
	})
	return err
}

// Delete removes a session and the coordination state that only makes
// sense while it exists. One transaction, because a session row that
// outlives its own inbox strands every sender waiting on a receipt.
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM review_targets WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_bases WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_scopes WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_states WHERE session_id = ?`, id); err != nil {
		return err
	}
	// Session ids are recycled from a fresh UUID prefix, so a message left
	// pointing at a deleted id could be re-attached to a future session.
	if _, err := tx.Exec(`DELETE FROM session_inbox WHERE session_id = ? OR sender_id = ?`, id, id); err != nil {
		return err
	}
	// A claim outlives its holder as pending work rather than as a task
	// parked forever against a session that no longer exists.
	if err := releaseTasksOwnedBy(tx, id, time.Now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM file_reservations WHERE session_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteChild removes a session only while it still hangs under parentID.
// kill runs inside the same write transaction, which holds the database's
// writer lock, so a placement cannot slip between the ownership check and
// the delete, and a failed kill rolls the row back into place.
func (s *Store) DeleteChild(id, parentID string, kill func() error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE sessions SET parent_id = ? WHERE id = ? AND parent_id = ?`, parentID, id, parentID)
	if err != nil {
		return err
	}
	held, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if held == 0 {
		return fmt.Errorf("session %s is no longer nested under %s", id, parentID)
	}
	if err := kill(); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_targets WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_bases WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_scopes WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_states WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
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
		if inSubtree(sess.Group, path) {
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
	likeOld := escapeLike(oldPath)
	_, err = tx.Exec(
		`UPDATE groups SET name = ? || substr(name, length(?)+1)
		 WHERE name = ? OR name LIKE ? || '/%' ESCAPE '\'`,
		newPath, oldPath, oldPath, likeOld)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`UPDATE sessions SET group_name = ? || substr(group_name, length(?)+1)
		 WHERE group_name = ? OR group_name LIKE ? || '/%' ESCAPE '\'`,
		newPath, oldPath, oldPath, likeOld)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MoveGroup re-parents a group subtree under newParent ("" = root),
// keeping its base name. Every descendant group and session follows.
func (s *Store) MoveGroup(path, newParent string) error {
	if path == "" {
		return fmt.Errorf("group path cannot be empty")
	}
	if inSubtree(newParent, path) {
		return fmt.Errorf("cannot move %s into its own subtree", path)
	}
	newPath := path[strings.LastIndex(path, "/")+1:]
	if newParent != "" {
		newPath = newParent + "/" + newPath
	}
	return s.RenameGroup(path, newPath)
}

// validParent reads the parent through the caller's transaction, so the row
// it approves cannot be deleted or nested before the write lands.
func validParent(tx *sql.Tx, id, parentID string) (string, error) {
	if parentID == id {
		return "", fmt.Errorf("session %s cannot be its own parent", id)
	}
	var group, grandparent string
	err := tx.QueryRow(`SELECT group_name, parent_id FROM sessions WHERE id = ?`, parentID).Scan(&group, &grandparent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("parent %s: %w", parentID, err)
	}
	if err != nil {
		return "", err
	}
	if grandparent != "" {
		return "", fmt.Errorf("parent %s already has a parent", parentID)
	}
	return group, nil
}

func (s *Store) PlaceSession(id, group, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if parentID != "" {
		parentGroup, err := validParent(tx, id, parentID)
		if err != nil {
			return err
		}
		group = parentGroup
	}
	if parentID != "" {
		var kids int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE parent_id = ?`, id).Scan(&kids); err != nil {
			return err
		}
		if kids > 0 {
			return fmt.Errorf("session %s has terminals of its own; move them out first", id)
		}
	}
	res, err := tx.Exec(
		`UPDATE sessions SET group_name = ?, parent_id = ?,
		 sort_order = (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ? AND parent_id = ?)
		 WHERE id = ?`,
		group, parentID, group, parentID, id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	if parentID == "" {
		if _, err := tx.Exec(`UPDATE sessions SET group_name = ? WHERE parent_id = ?`, group, id); err != nil {
			return err
		}
	}
	if group != "" {
		if _, err := tx.Exec(
			`INSERT INTO groups (name, sort_order)
			 VALUES (?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
			 ON CONFLICT(name) DO NOTHING`, group); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MoveSession(id, group string) error {
	return s.PlaceSession(id, group, "")
}

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

func (s *Store) RenameSessionWorktreeBranch(id, branch string) error {
	res, err := s.db.Exec(`UPDATE sessions SET worktree_branch = ? WHERE id = ?`, branch, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// UpdateTool changes which tool status rules and revive use for a session.
// Clears the captured agent conversation id: that id only makes sense for
// the tool that minted it, and a manual tool swap means the user swapped
// the process in the pane (e.g. quit opencode, ran grok). A no-op when the
// tool column already matches leaves the conversation id alone.
func (s *Store) UpdateTool(id, tool string) error {
	if strings.TrimSpace(tool) == "" {
		return fmt.Errorf("session tool cannot be empty")
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET tool = ?, agent_session_id = '' WHERE id = ? AND tool != ?`,
		tool, id, tool)
	if err != nil {
		return err
	}
	return s.requireRowOrNoop(res, id)
}

// DeleteGroup removes a group and all its descendant groups, reporting
// the paths it removed.
func (s *Store) DeleteGroup(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("cannot delete the root group")
	}
	groups, err := s.Groups()
	if err != nil {
		return nil, err
	}
	return s.deleteGroups(groups, func(g Group) bool { return inSubtree(g.Name, path) })
}

// ErrGroupNotFound reports a group path no row carries.
var ErrGroupNotFound = errors.New("group does not exist")

// RemoveGroup deletes a group and its descendant groups and moves every
// session held beneath them to the root, in one transaction, so a failure
// partway leaves the tree as it was. A moved session keeps its parent
// link, so a terminal relocated with its agent still hangs under it.
// Reports the group paths removed and the ids of the sessions moved.
func (s *Store) RemoveGroup(path string) (removedGroups, movedSessions []string, err error) {
	if path == "" {
		return nil, nil, fmt.Errorf("cannot delete the root group")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	groupNames, err := txStrings(tx, `SELECT name FROM groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, nil, err
	}
	known := false
	for _, name := range groupNames {
		if name == path {
			known = true
		}
		if inSubtree(name, path) {
			removedGroups = append(removedGroups, name)
		}
	}
	if !known {
		return nil, nil, ErrGroupNotFound
	}
	held, err := txStrings(tx,
		`SELECT id FROM sessions WHERE group_name = ? OR group_name LIKE ? || '/%' ESCAPE '\'
		 ORDER BY group_name, parent_id, sort_order`,
		path, escapeLike(path))
	if err != nil {
		return nil, nil, err
	}
	var nextOrder int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ''`,
	).Scan(&nextOrder); err != nil {
		return nil, nil, err
	}
	for _, id := range held {
		if _, err := tx.Exec(
			`UPDATE sessions SET group_name = '', sort_order = ? WHERE id = ?`, nextOrder, id); err != nil {
			return nil, nil, err
		}
		nextOrder++
	}
	for _, name := range removedGroups {
		if _, err := tx.Exec(`DELETE FROM groups WHERE name = ?`, name); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return removedGroups, held, nil
}

func txStrings(tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// PruneArchivedGroups removes the groups in a subtree that are archived,
// directly or through an archived ancestor, and hold no session anywhere
// beneath them, reporting the paths it removed. A group that still holds
// a session stays, and so does each of its ancestors, so no session is
// left without a home. Deleting a group from the archived view runs this
// instead of DeleteGroup: it clears the group rows that emptying the
// archive left behind while the live tree keeps its own.
func (s *Store) PruneArchivedGroups(root string) ([]string, error) {
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	occupied := map[string]bool{}
	for _, sess := range sessions {
		eachAncestor(sess.Group, func(path string) bool {
			occupied[path] = true
			return true
		})
	}
	groups, err := s.Groups()
	if err != nil {
		return nil, err
	}
	archived := map[string]bool{}
	for _, g := range groups {
		if g.Archived {
			archived[g.Name] = true
		}
	}
	return s.deleteGroups(groups, func(g Group) bool {
		return inSubtree(g.Name, root) && !occupied[g.Name] && EffectivelyArchived(archived, g.Name)
	})
}

// deleteGroups removes every group the predicate selects, reporting the
// paths it removed.
func (s *Store) deleteGroups(groups []Group, selected func(Group) bool) ([]string, error) {
	var removed []string
	for _, g := range groups {
		if !selected(g) {
			continue
		}
		if _, err := s.db.Exec(`DELETE FROM groups WHERE name = ?`, g.Name); err != nil {
			return removed, err
		}
		removed = append(removed, g.Name)
	}
	return removed, nil
}

// EffectivelyArchived reports whether a group path counts as archived,
// either directly or because an ancestor group was archived as a whole.
func EffectivelyArchived(archived map[string]bool, path string) bool {
	found := false
	eachAncestor(path, func(ancestor string) bool {
		found = archived[ancestor]
		return !found
	})
	return found
}

// inSubtree reports whether a group path is the root itself or any group
// nested under it.
func inSubtree(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

// eachAncestor visits a group path and then each of its ancestors,
// stopping early when the visitor returns false.
func eachAncestor(path string, visit func(string) bool) {
	for path != "" {
		if !visit(path) {
			return
		}
		idx := strings.LastIndex(path, "/")
		if idx < 0 {
			return
		}
		path = path[:idx]
	}
}

type Group struct {
	Name     string
	Path     string
	Archived bool
	// Worktree is the group's spawn-in-worktree choice: "on", "off", or
	// "" to inherit from the nearest ancestor with a choice, else the
	// global setting.
	Worktree string
}

func (s *Store) Groups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT name, path, archived, worktree FROM groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		var g Group
		var archived int
		if err := rows.Scan(&g.Name, &g.Path, &archived, &g.Worktree); err != nil {
			return nil, err
		}
		g.Archived = archived != 0
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// SetGroupWorktree stores a group's spawn-in-worktree choice: "on",
// "off", or "" to inherit.
func (s *Store) SetGroupWorktree(name, worktree string) error {
	_, err := s.db.Exec(`UPDATE groups SET worktree = ? WHERE name = ?`, worktree, name)
	return err
}

// SetGroupArchived flips the archived flag on a group, every descendant
// group, and every session in the subtree, in one transaction.
func (s *Store) SetGroupArchived(path string, archived bool) error {
	if path == "" {
		return fmt.Errorf("cannot archive the root group")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	flag := boolToInt(archived)
	likePath := escapeLike(path)
	if _, err := tx.Exec(
		`UPDATE groups SET archived = ? WHERE name = ? OR name LIKE ? || '/%' ESCAPE '\'`,
		flag, path, likePath); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE sessions SET archived = ? WHERE group_name = ? OR group_name LIKE ? || '/%' ESCAPE '\'`,
		flag, path, likePath); err != nil {
		return err
	}
	return tx.Commit()
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
		`SELECT id, archived FROM sessions WHERE group_name = ? AND parent_id = ?
		 ORDER BY sort_order, created_at`, sess.Group, sess.ParentID)
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
	ids := make([]string, len(siblings))
	for i, sibling := range siblings {
		ids[i] = sibling.id
	}
	if err := s.persistSessionOrder(ids); err != nil {
		return false, err
	}
	return true, nil
}

// SwapSessionOrder exchanges two sessions in the same group. The caller can
// choose visible siblings even when filtered sessions sit between them.
func (s *Store) SwapSessionOrder(id, targetID string) error {
	sess, err := s.Get(id)
	if err != nil {
		return err
	}
	target, err := s.Get(targetID)
	if err != nil {
		return err
	}
	if sess.Group != target.Group || sess.ParentID != target.ParentID {
		return fmt.Errorf("sessions %s and %s are not siblings", id, targetID)
	}

	rows, err := s.db.Query(
		`SELECT id FROM sessions WHERE group_name = ? AND parent_id = ?
		 ORDER BY sort_order, created_at`, sess.Group, sess.ParentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	current, targetIndex := -1, -1
	for rows.Next() {
		var siblingID string
		if err := rows.Scan(&siblingID); err != nil {
			return err
		}
		switch siblingID {
		case id:
			current = len(ids)
		case targetID:
			targetIndex = len(ids)
		}
		ids = append(ids, siblingID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if current < 0 || targetIndex < 0 {
		return fmt.Errorf("session order changed while reordering")
	}
	ids[current], ids[targetIndex] = ids[targetIndex], ids[current]
	return s.persistSessionOrder(ids)
}

func (s *Store) persistSessionOrder(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE sessions SET sort_order = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	if err := s.persistGroupOrder(groups); err != nil {
		return false, err
	}
	return true, nil
}

// SwapGroupOrder exchanges two groups with the same parent. siblingOrder
// materializes displayed ancestors so their manual order can persist too.
func (s *Store) SwapGroupOrder(path, targetPath string, siblingOrder ...string) error {
	if path == "" || targetPath == "" {
		return fmt.Errorf("cannot reorder the root group")
	}
	parent := parentPath(path)
	if parent != parentPath(targetPath) {
		return fmt.Errorf("groups %s and %s are not siblings", path, targetPath)
	}
	for _, sibling := range siblingOrder {
		if parentPath(sibling) != parent {
			return fmt.Errorf("group %s is not a sibling of %s", sibling, path)
		}
		if err := s.ensureGroup(sibling); err != nil {
			return err
		}
	}
	groups, err := s.Groups()
	if err != nil {
		return err
	}
	current, target := -1, -1
	for i, group := range groups {
		switch group.Name {
		case path:
			current = i
		case targetPath:
			target = i
		}
	}
	if current < 0 || target < 0 {
		return fmt.Errorf("group order changed while reordering")
	}
	groups[current], groups[target] = groups[target], groups[current]
	return s.persistGroupOrder(groups)
}

func parentPath(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx]
	}
	return ""
}

func (s *Store) persistGroupOrder(groups []Group) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, g := range groups {
		if _, err := tx.Exec(`UPDATE groups SET sort_order = ? WHERE name = ?`, i, g.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// requireRowOrNoop resolves a guarded UPDATE that matched zero rows: either
// the WHERE condition already held (success no-op) or the session is gone.
// Distinguish so callers still see ErrSessionGone.
func (s *Store) requireRowOrNoop(res sql.Result, id string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	var exists int
	err = s.db.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("session %s: %w", id, ErrSessionGone)
	}
	return err
}

func requireRow(res sql.Result, id string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("session %s: %w", id, ErrSessionGone)
	}
	return nil
}

// escapeLike escapes the LIKE metacharacters so a group path is matched
// literally in a `? || '/%'` prefix pattern, paired with ESCAPE '\'. Group
// names may contain '_' or '%', which LIKE would otherwise treat as
// wildcards and let one group's subtree bleed into another's.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
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
