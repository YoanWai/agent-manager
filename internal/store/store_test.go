package store

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestBrowserURLRoundTrips(t *testing.T) {
	st := newTestStore(t)
	sess := Session{ID: "b1", Name: "browser", Tool: "browser", Cwd: t.TempDir(), BrowserURL: "https://example.com"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("b1")
	if err != nil || got.BrowserURL != "https://example.com" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := st.SetBrowserURL("b1", "https://example.org/x"); err != nil {
		t.Fatal(err)
	}
	listed, err := st.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].BrowserURL != "https://example.org/x" {
		t.Fatalf("%+v", listed)
	}
}

// Environment profiles keep tokens in the database, so neither a fresh one
// nor one an earlier release created world-readable is left open to other
// local users, its WAL included.
func TestOpenLeavesTheDatabaseToItsOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file modes carry no group or other bits")
	}
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Close()
	if err := os.WriteFile(path+"-wal", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + "-wal"} {
		if err := os.Chmod(name, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if st, err = Open(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v; want 0600", filepath.Base(name), info, err)
		}
	}
}
