package store

import (
	"strings"

	_ "modernc.org/sqlite"
)

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
