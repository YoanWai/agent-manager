package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

// busyTimeout is how long a writer waits for the lock before giving up.
// The manager and every `agent-manager mcp` process share this database,
// so without it a collision between the poller's write and a session
// tool's write fails instantly with SQLITE_BUSY instead of waiting.
// _txlock=immediate takes the write lock at BEGIN, so a multi-statement
// transaction cannot fail halfway through trying to upgrade.
const busyTimeout = "?_pragma=busy_timeout(5000)&_txlock=immediate"

func Open(path string) (*Store, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve store path: %w", err)
	}
	// Use one lock identity when callers reach the same profile through a
	// symlinked directory. The DB itself may not exist yet, so resolve its
	// parent and retain the requested filename.
	if realPath, evalErr := filepath.EvalSymlinks(absPath); evalErr == nil {
		absPath = realPath
	} else if realDir, evalErr := filepath.EvalSymlinks(filepath.Dir(absPath)); evalErr == nil {
		absPath = filepath.Join(realDir, filepath.Base(absPath))
	}
	db, err := sql.Open("sqlite", absPath+busyTimeout)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db, path: absPath}
	if err := store.init(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.importConfigFile(filepath.Dir(path)); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
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
