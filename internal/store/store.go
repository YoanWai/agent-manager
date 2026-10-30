package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
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
	if err := restrictToOwner(absPath); err != nil {
		return nil, err
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

// restrictToOwner keeps the database readable by this user alone, since
// environment profiles store tokens in it. SQLite gives its WAL and shared
// memory files the database's own mode, so creating it 0600 covers them; a
// database from an earlier release, created 0644, is tightened in place.
func restrictToOwner(path string) error {
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	file.Close()
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(name, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("restrict store: %w", err)
		}
	}
	return nil
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
