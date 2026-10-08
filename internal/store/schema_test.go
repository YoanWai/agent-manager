package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A database from before the column reaches it through the migration, where
// its rows read back with the empty prompt that turns the wait off.
func TestLaunchPromptMigratesAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if _, err := legacy.Exec(`CREATE TABLE sessions (
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
	INSERT INTO sessions (id, name, tool, cwd, group_name, status, created_at, last_status_at)
	VALUES ('old', 'claude-1ff0', 'claude', '/tmp', '', 'idle', 0, 0)`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	old, err := s.Get("old")
	if err != nil {
		t.Fatalf("get migrated row: %v", err)
	}
	if old.LaunchPrompt != "" {
		t.Fatalf("migrated row launch prompt = %q, want empty", old.LaunchPrompt)
	}
	if err := s.CreateSession(Session{ID: "new", Name: "n", Tool: "claude", Cwd: "/tmp", LaunchPrompt: "/compact"}); err != nil {
		t.Fatalf("create after migration: %v", err)
	}
	got, err := s.Get("new")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LaunchPrompt != "/compact" {
		t.Fatalf("launch prompt = %q, want /compact", got.LaunchPrompt)
	}
}
