package store

import (
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
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

func TestUpdateTool(t *testing.T) {
	st := newTestStore(t)
	sess := sample("a", "g1")
	sess.Tool = "opencode"
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.SetAgentSessionID("a", "ses_old"); err != nil {
		t.Fatalf("set agent id: %v", err)
	}
	if err := st.UpdateTool("a", "grok"); err != nil {
		t.Fatalf("update tool: %v", err)
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tool != "grok" {
		t.Fatalf("tool = %q want grok", got.Tool)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("agent session id should clear on tool change, got %q", got.AgentSessionID)
	}
	if err := st.SetAgentSessionID("a", "ses_new"); err != nil {
		t.Fatalf("reset agent id: %v", err)
	}
	if err := st.UpdateTool("a", "grok"); err != nil {
		t.Fatalf("same-tool update: %v", err)
	}
	got, err = st.Get("a")
	if err != nil {
		t.Fatalf("get after same-tool: %v", err)
	}
	if got.AgentSessionID != "ses_new" {
		t.Fatalf("same-tool update wiped agent id: %q", got.AgentSessionID)
	}
	if err := st.UpdateTool("a", ""); err == nil {
		t.Fatal("empty tool should error")
	}
	if err := st.UpdateTool("missing", "claude"); err == nil {
		t.Fatal("update tool on missing row should error")
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
		"UpdateTool":        st.UpdateTool("a", "grok"),
		"Delete":            st.Delete("a"),
	}
	for name, err := range writes {
		if !errors.Is(err, ErrSessionGone) {
			t.Errorf("%s on a deleted session = %v, want ErrSessionGone", name, err)
		}
	}
}

func TestNoOpWriteDoesNotLookLikeADeletedSession(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "proj"))

	// Rewriting a column with the value it already holds still counts as a
	// row affected, so ErrSessionGone only ever means the row is absent.
	writes := map[string]error{
		"UpdateStatus":      st.UpdateStatus("a", "idle"),
		"SetAcked":          st.SetAcked("a", false),
		"SetAgentSessionID": st.SetAgentSessionID("a", ""),
		"SetSnapshot":       st.SetSnapshot("a", ""),
		"SetArchived":       st.SetArchived("a", false),
		"RenameSession":     st.RenameSession("a", "n-a"),
		"UpdateTool":        st.UpdateTool("a", "claude"),
	}
	for name, err := range writes {
		if err != nil {
			t.Errorf("%s writing an unchanged value = %v, want nil", name, err)
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

func TestSwapSessionOrderCrossesFilteredSibling(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	st.CreateSession(sample("hidden", "g1"))
	st.CreateSession(sample("c", "g1"))

	if err := st.SwapSessionOrder("c", "a"); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if got, want := listIDs(t, st, false), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v want %v", got, want)
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

func TestSwapGroupOrderCrossesFilteredSibling(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("alpha", "")
	st.CreateGroup("hidden", "")
	st.CreateGroup("gamma", "")

	if err := st.SwapGroupOrder("gamma", "alpha"); err != nil {
		t.Fatalf("swap: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	got := make([]string, len(groups))
	for i, group := range groups {
		got[i] = group.Name
	}
	if want := []string{"gamma", "hidden", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v want %v", got, want)
	}
}

func TestSwapGroupOrderMaterializesSyntheticAncestors(t *testing.T) {
	st := newTestStore(t)
	for _, group := range []string{"alpha/deep", "beta/deep", "gamma/deep"} {
		if err := st.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}

	if err := st.SwapGroupOrder("gamma", "beta", "alpha", "beta", "gamma"); err != nil {
		t.Fatalf("swap: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	var roots []string
	for _, group := range groups {
		if !strings.Contains(group.Name, "/") {
			roots = append(roots, group.Name)
		}
	}
	if want := []string{"alpha", "gamma", "beta"}; !slices.Equal(roots, want) {
		t.Fatalf("root order = %v want %v", roots, want)
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

// Deleting a session must take its review target with it, or a recycled
// session id would inherit a dead repo declaration.
func TestDeleteDropsReviewTarget(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewRepo("a", "/repos/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewRepo("a"); err != nil || got != "" {
		t.Fatalf("review target survived the delete: %q, %v", got, err)
	}
}

func TestReviewRepoRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if got, err := st.ReviewRepo("s1"); err != nil || got != "" {
		t.Fatalf("unset review repo = %q, %v; want empty, nil", got, err)
	}
	if err := st.SetReviewRepo("s1", "/repos/alpha"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewRepo("s1"); err != nil || got != "/repos/alpha" {
		t.Fatalf("review repo = %q, %v; want /repos/alpha", got, err)
	}
	if err := st.SetReviewRepo("s1", "/repos/bravo"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewRepo("s1"); got != "/repos/bravo" {
		t.Fatalf("review repo after update = %q, want /repos/bravo", got)
	}
	if err := st.SetReviewRepo("s1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewRepo("s1"); got != "" {
		t.Fatalf("review repo after clear = %q, want empty", got)
	}
}

// Deleting a session must take its review bases with it, or a recycled
// session id would inherit a dead base declaration.
func TestDeleteDropsReviewBase(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewBase("a", "/repos/alpha", "main"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewBase("a", "/repos/alpha"); err != nil || got != "" {
		t.Fatalf("review base survived the delete: %q, %v", got, err)
	}
}

func TestReviewBaseRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if got, err := st.ReviewBase("s1", "/repos/alpha"); err != nil || got != "" {
		t.Fatalf("unset review base = %q, %v; want empty, nil", got, err)
	}
	if err := st.SetReviewBase("s1", "/repos/alpha", "main"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewBase("s1", "/repos/alpha"); err != nil || got != "main" {
		t.Fatalf("review base = %q, %v; want main", got, err)
	}
	// A second repo under the same session keeps its own base.
	if err := st.SetReviewBase("s1", "/repos/bravo", "develop"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewBase("s1", "/repos/bravo"); got != "develop" {
		t.Fatalf("bravo base = %q, want develop", got)
	}
	if got, _ := st.ReviewBase("s1", "/repos/alpha"); got != "main" {
		t.Fatalf("alpha base after bravo set = %q, want main", got)
	}
	if err := st.SetReviewBase("s1", "/repos/alpha", "master"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewBase("s1", "/repos/alpha"); got != "master" {
		t.Fatalf("alpha base after update = %q, want master", got)
	}
	if err := st.SetReviewBase("s1", "/repos/alpha", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewBase("s1", "/repos/alpha"); got != "" {
		t.Fatalf("alpha base after clear = %q, want empty", got)
	}
}

func TestSessionWorktreeColumnsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	sess := Session{
		ID: "wt1", Name: "feat", Tool: "claude", Cwd: "/tmp/repo-worktrees/feat",
		WorktreeRepo: "/tmp/repo", WorktreeBranch: "am/feat",
	}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get("wt1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorktreeRepo != "/tmp/repo" || got.WorktreeBranch != "am/feat" {
		t.Fatalf("worktree fields lost: %+v", got)
	}
	list, err := s.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].WorktreeRepo != "/tmp/repo" || list[0].WorktreeBranch != "am/feat" {
		t.Fatalf("list dropped worktree fields: %+v", list[0])
	}
}

func TestMoveSessionWorktree(t *testing.T) {
	s := newTestStore(t)
	sess := Session{
		ID: "wt2", Name: "claude-7a72", Tool: "claude", Cwd: "/tmp/repo-worktrees/claude-7a72",
		WorktreeRepo: "/tmp/repo", WorktreeBranch: "am/claude-7a72",
	}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.MoveSessionWorktree("wt2", "/tmp/repo-worktrees/renamed", "am/renamed"); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, err := s.Get("wt2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Cwd != "/tmp/repo-worktrees/renamed" || got.WorktreeBranch != "am/renamed" {
		t.Fatalf("move did not land: %+v", got)
	}
	if got.WorktreeRepo != "/tmp/repo" {
		t.Fatalf("move disturbed the repo root: %q", got.WorktreeRepo)
	}
	if err := s.MoveSessionWorktree("ghost", "/tmp/x", "am/x"); err == nil {
		t.Fatal("moving an unknown session should error")
	}
}

func TestGroupWorktreeRoundtrip(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("backend", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Worktree != "" {
		t.Fatalf("new group should inherit worktree, got %+v", groups)
	}
	if err := st.SetGroupWorktree("backend", "on"); err != nil {
		t.Fatalf("set: %v", err)
	}
	groups, err = st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if groups[0].Worktree != "on" {
		t.Fatalf("worktree choice lost: %+v", groups[0])
	}
	if err := st.SetGroupWorktree("backend", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	groups, err = st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if groups[0].Worktree != "" {
		t.Fatalf("worktree choice should clear back to inherit: %+v", groups[0])
	}
}

func TestAddGroupStoresSettingsWithoutReplacingExistingGroup(t *testing.T) {
	st := newTestStore(t)
	if err := st.AddGroup("backend", "/first", "off"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := st.AddGroup("backend", "/second", "on"); !errors.Is(err, ErrGroupExists) {
		t.Fatalf("duplicate add error = %v, want ErrGroupExists", err)
	}

	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Path != "/first" || groups[0].Worktree != "off" {
		t.Fatalf("duplicate add changed group: %+v", groups)
	}
}

func TestRestartAgentRetiresTheConversationItReplaces(t *testing.T) {
	st := newTestStore(t)
	sess := sample("a", "g1")
	sess.AgentSessionID = "first"
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	created, err := st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if !created.LaunchTime().Equal(created.CreatedAt) {
		t.Fatalf("launch time before any restart = %v, want the creation time %v", created.LaunchTime(), created.CreatedAt)
	}

	firstRestart := created.CreatedAt.Add(time.Minute)
	if err := st.RestartAgent("a", "second", firstRestart); err != nil {
		t.Fatalf("restart: %v", err)
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "second" || got.RetiredAgentSessionID != "first" {
		t.Fatalf("after restart: id = %q, retired = %q", got.AgentSessionID, got.RetiredAgentSessionID)
	}
	if !got.LaunchTime().Equal(firstRestart) {
		t.Fatalf("launch time = %v, want %v", got.LaunchTime(), firstRestart)
	}

	// A tool that mints its own id restarts with no id to record; the
	// conversation it just left is still the one to keep out of capture.
	secondRestart := firstRestart.Add(time.Minute)
	if err := st.RestartAgent("a", "", secondRestart); err != nil {
		t.Fatalf("restart: %v", err)
	}
	got, err = st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" || got.RetiredAgentSessionID != "second" {
		t.Fatalf("after second restart: id = %q, retired = %q", got.AgentSessionID, got.RetiredAgentSessionID)
	}

	// A restart that follows without a conversation to retire keeps the
	// last real one rather than forgetting it.
	if err := st.RestartAgent("a", "", secondRestart.Add(time.Minute)); err != nil {
		t.Fatalf("restart: %v", err)
	}
	got, err = st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.RetiredAgentSessionID != "second" {
		t.Fatalf("retired = %q, want it kept", got.RetiredAgentSessionID)
	}
}
