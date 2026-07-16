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

func TestCreateAndList(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.CreateSession(sample("b", "g2")); err != nil {
		t.Fatalf("create: %v", err)
	}
	sessions, err := st.ListSessions(false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
}

func TestArchiveHidesFromActiveList(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	if err := st.SetArchived("a", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	active, _ := st.ListSessions(false)
	if len(active) != 0 {
		t.Fatalf("archived session should not appear in active list, got %d", len(active))
	}
	all, _ := st.ListSessions(true)
	if len(all) != 1 || !all[0].Archived {
		t.Fatalf("archived session should appear in full list as archived")
	}
	if err := st.SetArchived("a", false); err != nil {
		t.Fatalf("restore: %v", err)
	}
	active, _ = st.ListSessions(false)
	if len(active) != 1 {
		t.Fatalf("restore should return session to active list")
	}
}

func TestUpdateStatus(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	if err := st.UpdateStatus("a", "working"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "working" {
		t.Fatalf("status = %q want working", got.Status)
	}
}

func TestDelete(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	if err := st.Delete("a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Delete("a"); err == nil {
		t.Fatal("deleting missing session should error")
	}
}

func TestMissingRowErrors(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpdateStatus("nope", "x"); err == nil {
		t.Fatal("update on missing row should error")
	}
	if err := st.SetArchived("nope", true); err == nil {
		t.Fatal("archive on missing row should error")
	}
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

func TestReorderSession(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	st.CreateSession(sample("b", "g1"))
	st.CreateSession(sample("c", "g1"))

	if moved, err := st.ReorderSession("c", -1, false); err != nil || !moved {
		t.Fatalf("reorder: moved=%v err=%v", moved, err)
	}
	got := listIDs(t, st, false)
	want := []string{"a", "c", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v want %v", got, want)
		}
	}

	// Top of group: no-op, no error.
	if moved, err := st.ReorderSession("a", -1, false); err != nil || moved {
		t.Fatalf("edge reorder: moved=%v err=%v, want no-op", moved, err)
	}
	if ids := listIDs(t, st, false); ids[0] != "a" {
		t.Fatalf("edge move should keep order, got %v", ids)
	}
}

func TestReorderSessionSkipsArchivedInActiveView(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	st.CreateSession(sample("b", "g1"))
	st.CreateSession(sample("c", "g1"))
	st.SetArchived("b", true)

	if moved, err := st.ReorderSession("c", -1, false); err != nil || !moved {
		t.Fatalf("reorder: moved=%v err=%v", moved, err)
	}
	got := listIDs(t, st, false)
	if got[0] != "c" || got[1] != "a" {
		t.Fatalf("c should jump over hidden b to swap with a, got %v", got)
	}
}

func TestReorderGroup(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("alpha", "")
	st.CreateGroup("beta", "")
	st.CreateGroup("alpha/sub", "")

	if moved, err := st.ReorderGroup("beta", -1); err != nil || !moved {
		t.Fatalf("reorder: moved=%v err=%v", moved, err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	posOf := func(name string) int {
		for i, g := range groups {
			if g.Name == name {
				return i
			}
		}
		return -1
	}
	if posOf("beta") > posOf("alpha") {
		t.Fatalf("beta should come before alpha, got %v", groups)
	}

	// Nested group only swaps with same-parent siblings; sole child is a no-op.
	if moved, err := st.ReorderGroup("alpha/sub", -1); err != nil || moved {
		t.Fatalf("nested sole child: moved=%v err=%v, want no-op", moved, err)
	}
}
