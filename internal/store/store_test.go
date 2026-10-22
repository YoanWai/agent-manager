package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sample(id, group string) Session {
	return Session{ID: id, Name: "n-" + id, Tool: "claude", Cwd: "/tmp", Group: group, Status: "idle"}
}
