package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
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

// A message queued before the subject column existed reads back with none,
// and one queued after keeps the subject it was sent with.
func TestInboxSubjectMigratesAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if _, err := legacy.Exec(`CREATE TABLE session_inbox (
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
	);
	INSERT INTO session_inbox (session_id, sender_id, sender_name, body, fingerprint, sent_at)
	VALUES ('target01', 'sender01', 'reviewer', 'old body', 'old', 1)`); err != nil {
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

	old, err := s.LoggedMessage(1)
	if err != nil {
		t.Fatalf("read migrated message: %v", err)
	}
	if old.Subject != "" || old.Body != "old body" {
		t.Fatalf("migrated message = %+v, want the old body and no subject", old)
	}
	fresh := message("new body", time.Now())
	fresh.Subject = "review report"
	id, err := s.Enqueue(fresh, DefaultInboxLimits)
	if err != nil {
		t.Fatalf("enqueue after migration: %v", err)
	}
	got, err := s.LoggedMessage(id)
	if err != nil || got.Subject != "review report" {
		t.Fatalf("subject after migration = %q, err %v", got.Subject, err)
	}
}
