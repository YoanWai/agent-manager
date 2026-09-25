package store

import (
	"path/filepath"
	"sort"
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

func listIDs(t *testing.T, st *Store, includeArchived bool) []string {
	t.Helper()
	sessions, err := st.ListSessions(includeArchived)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, len(sessions))
	for i, sess := range sessions {
		ids[i] = sess.ID
	}
	return ids
}

func groupArchived(t *testing.T, st *Store, path string) bool {
	t.Helper()
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	for _, g := range groups {
		if g.Name == path {
			return g.Archived
		}
	}
	t.Fatalf("group %q not found", path)
	return false
}

func groupPaths(t *testing.T, st *Store) []string {
	t.Helper()
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	paths := make([]string, len(groups))
	for i, g := range groups {
		paths[i] = g.Name
	}
	sort.Strings(paths)
	return paths
}

func sessionArchived(t *testing.T, st *Store, id string) bool {
	t.Helper()
	sess, err := st.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return sess.Archived
}
