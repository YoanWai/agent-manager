package store

import (
	"errors"
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

func TestSetGroupArchivedFlipsSubtree(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/sub", "")
	st.CreateSession(sample("a", "proj"))
	st.CreateSession(sample("b", "proj/sub"))

	if err := st.SetGroupArchived("proj", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}
	if !groupArchived(t, st, "proj") || !groupArchived(t, st, "proj/sub") {
		t.Fatal("group and subgroup should be archived")
	}
	if !sessionArchived(t, st, "a") || !sessionArchived(t, st, "b") {
		t.Fatal("sessions in subtree should be archived")
	}

	if err := st.SetGroupArchived("proj", false); err != nil {
		t.Fatalf("restore group: %v", err)
	}
	if groupArchived(t, st, "proj") || groupArchived(t, st, "proj/sub") {
		t.Fatal("group and subgroup should be restored")
	}
	if sessionArchived(t, st, "a") || sessionArchived(t, st, "b") {
		t.Fatal("sessions in subtree should be restored")
	}
}

func TestSetGroupArchivedUnderscoreDoesNotBleed(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("my_proj", "")
	st.CreateGroup("myXproj/sub", "")
	st.CreateSession(sample("a", "my_proj"))
	st.CreateSession(sample("b", "myXproj/sub"))

	if err := st.SetGroupArchived("my_proj", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !groupArchived(t, st, "my_proj") || !sessionArchived(t, st, "a") {
		t.Fatal("my_proj and its session should be archived")
	}
	// "my_proj/%" with an unescaped underscore matches "myXproj/sub".
	if groupArchived(t, st, "myXproj/sub") || sessionArchived(t, st, "b") {
		t.Fatal("myXproj/sub must not be caught by the my_proj archive (LIKE _ wildcard bleed)")
	}
}

func TestPruneArchivedGroupsRemovesOnlyEmptyArchivedOnes(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/gone", "")
	st.CreateGroup("proj/live", "")
	st.CreateSession(sample("a", "proj/live"))
	if err := st.SetGroupArchived("proj/gone", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}

	removed, err := st.PruneArchivedGroups("proj")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != "proj/gone" {
		t.Fatalf("removed = %v want [proj/gone]", removed)
	}
	paths := groupPaths(t, st)
	if len(paths) != 2 || paths[0] != "proj" || paths[1] != "proj/live" {
		t.Fatalf("remaining groups = %v want [proj proj/live]", paths)
	}
}

func TestPruneArchivedGroupsKeepsArchivedGroupHoldingASession(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/sub", "")
	if err := st.SetGroupArchived("proj", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}
	// A session launched into an archived group starts out live, so neither
	// its group nor that group's ancestors may be pruned away under it.
	st.CreateSession(sample("a", "proj/sub"))

	removed, err := st.PruneArchivedGroups("proj")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v want none", removed)
	}
	if paths := groupPaths(t, st); len(paths) != 2 {
		t.Fatalf("remaining groups = %v want both kept", paths)
	}
}

func TestWritesToADeletedSessionReportGone(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "proj"))
	if err := st.Delete("a"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	writes := map[string]error{
		"UpdateStatus":      st.UpdateStatus("a", "idle"),
		"SetAcked":          st.SetAcked("a", true),
		"SetAgentSessionID": st.SetAgentSessionID("a", "conv"),
		"SetSnapshot":       st.SetSnapshot("a", "pane"),
		"SetArchived":       st.SetArchived("a", true),
		"RenameSession":     st.RenameSession("a", "renamed"),
		"Delete":            st.Delete("a"),
	}
	for name, err := range writes {
		if !errors.Is(err, ErrSessionGone) {
			t.Errorf("%s on a deleted session = %v, want ErrSessionGone", name, err)
		}
	}
}

func TestSetGroupArchivedEmptyPathErrors(t *testing.T) {
	st := newTestStore(t)
	if err := st.SetGroupArchived("", true); err == nil {
		t.Fatal("archiving the root group should error")
	}
}

func TestRestoreSessionUnarchivesAncestorGroups(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("proj", "")
	st.CreateGroup("proj/sub", "")
	st.CreateSession(sample("a", "proj/sub"))
	if err := st.SetGroupArchived("proj", true); err != nil {
		t.Fatalf("archive group: %v", err)
	}

	if err := st.SetArchived("a", false); err != nil {
		t.Fatalf("restore session: %v", err)
	}
	if sessionArchived(t, st, "a") {
		t.Fatal("session should be active after restore")
	}
	if groupArchived(t, st, "proj") || groupArchived(t, st, "proj/sub") {
		t.Fatal("ancestor groups should be un-archived so the session has a live home")
	}
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

func TestSetAcked(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	if err := st.SetAcked("a", true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	got, _ := st.Get("a")
	if !got.Acked {
		t.Fatal("acked should persist")
	}
	if err := st.SetAcked("a", false); err != nil {
		t.Fatalf("clear acked: %v", err)
	}
	got, _ = st.Get("a")
	if got.Acked {
		t.Fatal("acked should clear")
	}
}

func TestAgentSessionIDRoundTrip(t *testing.T) {
	st := newTestStore(t)
	sess := sample("a", "g1")
	sess.AgentSessionID = "conv-uuid-1"
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AgentSessionID != "conv-uuid-1" {
		t.Fatalf("stored agent id = %q, want conv-uuid-1", got.AgentSessionID)
	}

	if err := st.SetAgentSessionID("a", "conv-uuid-2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AgentSessionID != "conv-uuid-2" {
		t.Fatalf("updated agent id = %q, want conv-uuid-2", got.AgentSessionID)
	}

	list, err := st.ListSessions(false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].AgentSessionID != "conv-uuid-2" {
		t.Fatalf("list agent id = %+v", list)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := newTestStore(t)
	sess := Session{ID: "snap1", Name: "one", Tool: "claude", Cwd: "/tmp"}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if snapshot, err := s.Snapshot("snap1"); err != nil || snapshot != "" {
		t.Fatalf("fresh session snapshot = %q, %v; want empty", snapshot, err)
	}
	if err := s.SetSnapshot("snap1", "pane\x1b[31mtext\x1b[0m"); err != nil {
		t.Fatalf("set: %v", err)
	}
	snapshot, err := s.Snapshot("snap1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if snapshot != "pane\x1b[31mtext\x1b[0m" {
		t.Fatalf("snapshot = %q", snapshot)
	}
	if err := s.SetSnapshot("missing", "x"); err == nil {
		t.Fatal("SetSnapshot on a missing session should fail")
	}
	if snapshot, err := s.Snapshot("missing"); err != nil || snapshot != "" {
		t.Fatalf("missing session snapshot = %q, %v; want empty, nil", snapshot, err)
	}
}
