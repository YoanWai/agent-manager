package store

import (
	"database/sql"
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

func TestPendingInputsPersistAndConsumeInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sess := sample("a", "g1")
	sess.PendingInputs = []string{"first", "second"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st.Close()
	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !slices.Equal(got.PendingInputs, []string{"first", "second"}) {
		t.Fatalf("pending inputs = %q", got.PendingInputs)
	}
	claimed, err := st.ClaimPendingInput("a", "second")
	if err != nil || claimed {
		t.Fatalf("claim out of order = %v, %v", claimed, err)
	}
	claimed, err = st.ClaimPendingInput("a", "first")
	if err != nil || !claimed {
		t.Fatalf("claim first = %v, %v", claimed, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close claimed store: %v", err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen claimed store: %v", err)
	}
	defer st.Close()
	got, err = st.Get("a")
	if err != nil {
		t.Fatalf("get claimed: %v", err)
	}
	if !got.PendingInputClaimed {
		t.Fatal("pending delivery claim did not survive reopen")
	}
	consumed, err := st.ConsumeClaimedPendingInput("a", "first")
	if err != nil || !consumed {
		t.Fatalf("consume first = %v, %v", consumed, err)
	}
	got, err = st.Get("a")
	if err != nil {
		t.Fatalf("get after consume: %v", err)
	}
	if !slices.Equal(got.PendingInputs, []string{"second"}) {
		t.Fatalf("remaining inputs = %q", got.PendingInputs)
	}
	if got.PendingInputClaimed {
		t.Fatal("delivery claim remained after consumption")
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

// Delete strips a session's inbox, task claims and reservations before the
// row itself, so a delete that gets partway through would leave a session
// alive with the state its senders and the shared list depend on already
// gone. It is one transaction, and a refused delete changes nothing.
func TestARefusedDeleteLeavesTheCoordinationStateIntact(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.Enqueue(InboxMessage{
		SessionID: "a", SenderID: "b", SenderName: "rival",
		Body: "rebase on main", Fingerprint: "rebase on main", SentAt: now,
	}, DefaultInboxLimits); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := st.CreateTask(Task{ID: "t1", Title: "wire it", State: TaskPending, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if claimed, err := st.ClaimTask("t1", "a", now); err != nil || !claimed {
		t.Fatalf("ClaimTask = %v, %v", claimed, err)
	}
	if _, err := st.Reserve([]Reservation{{
		ID: "r1", SessionID: "a", Pattern: "internal/store/*.go", Mode: ReservationExclusive,
		AcquiredAt: now, ExpiresAt: now.Add(time.Hour),
	}}); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// The row alone going first is what the last statement refusing looks
	// like from inside Delete.
	if _, err := st.db.Exec(`DELETE FROM sessions WHERE id = ?`, "a"); err != nil {
		t.Fatalf("drop the session row: %v", err)
	}
	if err := st.Delete("a"); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("deleting a vanished session = %v", err)
	}

	queued, err := st.QueuedCount("a")
	if err != nil {
		t.Fatalf("QueuedCount: %v", err)
	}
	if queued != 1 {
		t.Fatalf("a refused delete took the inbox with it: %d queued", queued)
	}
	task, err := st.Task("t1")
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.State != TaskInProgress || task.Owner != "a" {
		t.Fatalf("a refused delete released the claim: %+v", task)
	}
	held, err := st.Reservations(now)
	if err != nil {
		t.Fatalf("Reservations: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("a refused delete dropped the leases: %+v", held)
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
		"UpdateStatus":       st.UpdateStatus("a", "idle"),
		"SetAcked":           st.SetAcked("a", true),
		"SetAgentSessionID":  st.SetAgentSessionID("a", "conv"),
		"SetAgentLaunchedAt": st.SetAgentLaunchedAt("a", time.Now()),
		"SetSnapshot":        st.SetSnapshot("a", "pane"),
		"SetArchived":        st.SetArchived("a", true),
		"RenameSession":      st.RenameSession("a", "renamed"),
		"UpdateTool":         st.UpdateTool("a", "grok"),
		"Delete":             st.Delete("a"),
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
		"UpdateStatus":       st.UpdateStatus("a", "idle"),
		"SetAcked":           st.SetAcked("a", false),
		"SetAgentSessionID":  st.SetAgentSessionID("a", ""),
		"SetAgentLaunchedAt": st.SetAgentLaunchedAt("a", time.Time{}),
		"SetSnapshot":        st.SetSnapshot("a", ""),
		"SetArchived":        st.SetArchived("a", false),
		"RenameSession":      st.RenameSession("a", "n-a"),
		"UpdateTool":         st.UpdateTool("a", "claude"),
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

func TestLaunchPromptRoundTrip(t *testing.T) {
	s := newTestStore(t)
	prompt := "/create-jira-issue plan the sprint"
	if err := s.CreateSession(Session{ID: "lp1", Name: "claude-1ff0", Tool: "claude", Cwd: "/tmp", LaunchPrompt: prompt}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get("lp1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LaunchPrompt != prompt {
		t.Fatalf("launch prompt = %q, want %q", got.LaunchPrompt, prompt)
	}
	list, err := s.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].LaunchPrompt != prompt {
		t.Fatalf("list dropped the launch prompt: %+v", list[0])
	}
}

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

func TestSetAgentLaunchedAtMovesLaunchTimeWithoutRetiringConversation(t *testing.T) {
	st := newTestStore(t)
	sess := sample("a", "g1")
	sess.AgentSessionID = "kept"
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	created, err := st.Get("a")
	if err != nil {
		t.Fatal(err)
	}

	launchedAt := created.CreatedAt.Add(5 * 24 * time.Hour).In(time.FixedZone("east", 9*3600))
	if err := st.SetAgentLaunchedAt("a", launchedAt); err != nil {
		t.Fatalf("launch: %v", err)
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "kept" || got.RetiredAgentSessionID != "" {
		t.Fatalf("conversation = %q retired = %q, want kept and empty", got.AgentSessionID, got.RetiredAgentSessionID)
	}
	if !got.LaunchTime().Equal(launchedAt) {
		t.Fatalf("launch time = %v, want %v", got.LaunchTime(), launchedAt)
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

// Capture answers a launch that may already be over by the time it lands, so
// binding is a compare-and-set: the row must still be unbound and still carry
// the launch the capture ran for.
func TestBindAgentSessionIDOnlyBindsTheLaunchItAnswers(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	created, err := st.Get("a")
	if err != nil {
		t.Fatal(err)
	}

	// A session that never restarted stores a zero launch time, and the
	// capture that read it carries that same zero.
	bound, err := st.BindAgentSessionID("a", "first", created.AgentLaunchedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Fatal("a capture for the current launch should bind")
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "first" {
		t.Fatalf("conversation id = %q, want first", got.AgentSessionID)
	}

	// A second answer for the same launch arrives after the row is bound.
	bound, err = st.BindAgentSessionID("a", "second", created.AgentLaunchedAt)
	if err != nil {
		t.Fatal(err)
	}
	if bound {
		t.Fatal("a bound row must not take another capture")
	}

	// A restart clears the id and moves the launch on; an answer from the
	// launch before it names the conversation the restart dropped.
	restarted := created.CreatedAt.Add(time.Minute)
	if err := st.RestartAgent("a", "", restarted); err != nil {
		t.Fatal(err)
	}
	bound, err = st.BindAgentSessionID("a", "stale", created.AgentLaunchedAt)
	if err != nil {
		t.Fatal(err)
	}
	if bound {
		t.Fatal("a capture from the launch before the restart must not bind")
	}
	bound, err = st.BindAgentSessionID("a", "fresh", restarted)
	if err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Fatal("a capture for the launch now running should bind")
	}
	got, err = st.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "fresh" {
		t.Fatalf("conversation id = %q, want fresh", got.AgentSessionID)
	}
}

func TestMoveGroupReparentsSubtree(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", "/srv/inner"); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateGroup("alpha/inner/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateGroup("beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.SetGroupWorktree("alpha/inner", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	if err := st.CreateSession(sample("a", "alpha/inner")); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.CreateSession(sample("b", "alpha/inner/deep")); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", "beta"); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	byName := make(map[string]Group, len(groups))
	for _, group := range groups {
		if group.Name == "alpha/inner" || group.Name == "alpha/inner/deep" {
			t.Fatalf("old group path %s still present", group.Name)
		}
		byName[group.Name] = group
	}
	moved, ok := byName["beta/inner"]
	if !ok {
		t.Fatal("beta/inner missing")
	}
	if moved.Path != "/srv/inner" {
		t.Fatalf("beta/inner path = %q, want /srv/inner", moved.Path)
	}
	if moved.Worktree != "on" {
		t.Fatalf("beta/inner worktree = %q, want on", moved.Worktree)
	}
	if _, ok := byName["beta/inner/deep"]; !ok {
		t.Fatal("beta/inner/deep missing")
	}
	for id, want := range map[string]string{"a": "beta/inner", "b": "beta/inner/deep"} {
		sess, err := st.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if sess.Group != want {
			t.Fatalf("session %s group = %q, want %q", id, sess.Group, want)
		}
	}
}

func TestMoveGroupToRoot(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", ""); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	found := false
	for _, group := range groups {
		if group.Name == "inner" {
			found = true
		}
	}
	if !found {
		t.Fatal("group inner missing at root")
	}
}

func TestMoveGroupIntoOwnSubtreeFails(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha", "alpha/inner"); err == nil {
		t.Fatal("moving a group into its own subtree should fail")
	}
	if err := st.MoveGroup("alpha", "alpha"); err == nil {
		t.Fatal("moving a group into itself should fail")
	}
}

func TestMoveGroupNameCollisionFails(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateGroup("beta/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", "beta"); err == nil {
		t.Fatal("moving onto an existing sibling name should fail")
	}
}

func TestMoveGroupSameParentIsNoop(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.MoveGroup("alpha/inner", "alpha"); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}
}

func TestParentIDRoundTrip(t *testing.T) {
	st := newTestStore(t)
	parent := sample("agent", "g1")
	if err := st.CreateSession(parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child := sample("sh", "g1")
	child.Tool = "terminal"
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	got, err := st.Get("sh")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ParentID != "agent" {
		t.Fatalf("ParentID = %q, want agent", got.ParentID)
	}
	list, err := st.ListSessions(false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]Session{}
	for _, sess := range list {
		byID[sess.ID] = sess
	}
	if byID["sh"].ParentID != "agent" || byID["agent"].ParentID != "" {
		t.Fatalf("list parent ids: %+v", byID)
	}
}

func TestChildrenIncludesArchivedAndOrder(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("parent: %v", err)
	}
	first := sample("a", "g")
	first.ParentID = "agent"
	second := sample("b", "g")
	second.ParentID = "agent"
	if err := st.CreateSession(first); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.CreateSession(second); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := st.SetArchived("a", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	kids, err := st.Children("agent")
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 2 || kids[0].ID != "a" || kids[1].ID != "b" {
		t.Fatalf("children = %+v", kids)
	}
}

func TestDeleteParentLeavesChildRow(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("parent: %v", err)
	}
	child := sample("sh", "g")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := st.Delete("agent"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := st.Get("sh")
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if got.ParentID != "agent" {
		t.Fatalf("store must not cascade, ParentID = %q", got.ParentID)
	}
}

func TestPlaceSessionNestsAndUnnests(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("sh", "g1")); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if err := st.PlaceSession("sh", "g2", "agent"); err != nil {
		t.Fatalf("nest: %v", err)
	}
	got, err := st.Get("sh")
	if err != nil || got.ParentID != "agent" || got.Group != "g1" {
		t.Fatalf("nested = %+v err %v", got, err)
	}
	if err := st.PlaceSession("sh", "g2", ""); err != nil {
		t.Fatalf("unnest: %v", err)
	}
	got, err = st.Get("sh")
	if err != nil || got.ParentID != "" || got.Group != "g2" {
		t.Fatalf("unnested = %+v err %v", got, err)
	}
}

func TestPlaceSessionRejectsBadParent(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	child := sample("mid", "g")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("mid: %v", err)
	}
	if err := st.CreateSession(sample("sh", "g")); err != nil {
		t.Fatalf("sh: %v", err)
	}
	if err := st.PlaceSession("sh", "g", "missing"); err == nil {
		t.Fatal("missing parent")
	}
	if err := st.PlaceSession("agent", "g", "agent"); err == nil {
		t.Fatal("self parent")
	}
	if err := st.PlaceSession("sh", "g", "mid"); err == nil {
		t.Fatal("parent that already has a parent")
	}
}

func TestCreateSessionRejectsBadParent(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	nested := sample("mid", "g1")
	nested.ParentID = "agent"
	if err := st.CreateSession(nested); err != nil {
		t.Fatalf("mid: %v", err)
	}
	missing := sample("sh1", "g1")
	missing.ParentID = "gone"
	if err := st.CreateSession(missing); err == nil {
		t.Fatal("missing parent")
	}
	self := sample("sh2", "g1")
	self.ParentID = "sh2"
	if err := st.CreateSession(self); err == nil {
		t.Fatal("self parent")
	}
	grandchild := sample("sh3", "g1")
	grandchild.ParentID = "mid"
	if err := st.CreateSession(grandchild); err == nil {
		t.Fatal("parent that already has a parent")
	}
	elsewhere := sample("sh4", "g2")
	elsewhere.ParentID = "agent"
	if err := st.CreateSession(elsewhere); err != nil {
		t.Fatalf("child in another group: %v", err)
	}
	got, err := st.Get("sh4")
	if err != nil || got.Group != "g1" {
		t.Fatalf("child group = %+v err %v", got, err)
	}
}

func TestDeleteChildAuthorizesKeepsAndCleans(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("other", "g1")); err != nil {
		t.Fatalf("other: %v", err)
	}
	child := sample("sh", "g1")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := st.SetReviewRepo("sh", "/repo"); err != nil {
		t.Fatalf("review repo: %v", err)
	}
	if err := st.SetReviewBase("sh", "/repo", "origin/main"); err != nil {
		t.Fatalf("review base: %v", err)
	}
	if err := st.SetReviewScope("sh", "staged"); err != nil {
		t.Fatalf("review scope: %v", err)
	}
	if err := st.SetReviewState("sh", "/repo", ReviewState{Round: ReviewRound{Number: 1}}); err != nil {
		t.Fatalf("review state: %v", err)
	}
	if err := st.DeleteChild("sh", "other", func() error {
		t.Fatal("kill ran for another session's terminal")
		return nil
	}); err == nil {
		t.Fatal("deleted a terminal nested elsewhere")
	}
	killErr := errors.New("kill refused")
	if err := st.DeleteChild("sh", "agent", func() error { return killErr }); !errors.Is(err, killErr) {
		t.Fatalf("kill error = %v", err)
	}
	got, err := st.Get("sh")
	if err != nil || got.ParentID != "agent" {
		t.Fatalf("row after failed kill = %+v err %v", got, err)
	}
	if repo, err := st.ReviewRepo("sh"); err != nil || repo != "/repo" {
		t.Fatalf("review repo after failed kill = %q err %v", repo, err)
	}
	if base, err := st.ReviewBase("sh", "/repo"); err != nil || base != "origin/main" {
		t.Fatalf("review base after failed kill = %q err %v", base, err)
	}
	if scope, err := st.ReviewScope("sh"); err != nil || scope != "staged" {
		t.Fatalf("review scope after failed kill = %q err %v", scope, err)
	}
	if state, err := st.ReviewState("sh", "/repo"); err != nil || state.Round.Number != 1 {
		t.Fatalf("review state after failed kill = %+v err %v", state, err)
	}
	killed := false
	if err := st.DeleteChild("sh", "agent", func() error { killed = true; return nil }); err != nil {
		t.Fatalf("DeleteChild: %v", err)
	}
	if !killed {
		t.Fatal("kill never ran")
	}
	if _, err := st.Get("sh"); err == nil {
		t.Fatal("row still present")
	}
	if repo, err := st.ReviewRepo("sh"); err != nil || repo != "" {
		t.Fatalf("review repo = %q err %v", repo, err)
	}
	if base, err := st.ReviewBase("sh", "/repo"); err != nil || base != "" {
		t.Fatalf("review base = %q err %v", base, err)
	}
	if scope, err := st.ReviewScope("sh"); err != nil || scope != "" {
		t.Fatalf("review scope = %q err %v", scope, err)
	}
	if state, err := st.ReviewState("sh", "/repo"); err != nil || state.Round.Number != 0 {
		t.Fatalf("review state = %+v err %v", state, err)
	}
}

func TestCreateSessionBesideFollowsTheAnchorPlacement(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	anchor := sample("sh", "g1")
	anchor.ParentID = "agent"
	if err := st.CreateSession(anchor); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	sibling := sample("sh2", "g2")
	if err := st.CreateSessionBeside(sibling, "sh"); err != nil {
		t.Fatalf("beside: %v", err)
	}
	got, err := st.Get("sh2")
	if err != nil || got.ParentID != "agent" || got.Group != "g1" {
		t.Fatalf("sibling = %+v err %v", got, err)
	}
	if err := st.PlaceSession("sh", "g3", ""); err != nil {
		t.Fatalf("unnest anchor: %v", err)
	}
	loose := sample("sh3", "g1")
	if err := st.CreateSessionBeside(loose, "sh"); err != nil {
		t.Fatalf("beside un-nested: %v", err)
	}
	got, err = st.Get("sh3")
	if err != nil || got.ParentID != "" || got.Group != "g3" {
		t.Fatalf("un-nested sibling = %+v err %v", got, err)
	}
	if err := st.CreateSessionBeside(sample("sh4", "g1"), "gone"); err == nil {
		t.Fatal("created beside a missing anchor")
	}
}

func TestPlaceSessionReparentsBetweenAgents(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("first", "g1")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.CreateSession(sample("second", "g2")); err != nil {
		t.Fatalf("second: %v", err)
	}
	held := sample("held", "g2")
	held.ParentID = "second"
	if err := st.CreateSession(held); err != nil {
		t.Fatalf("held: %v", err)
	}
	moved := sample("moved", "g1")
	moved.ParentID = "first"
	if err := st.CreateSession(moved); err != nil {
		t.Fatalf("moved: %v", err)
	}
	if err := st.PlaceSession("moved", "g1", "second"); err != nil {
		t.Fatalf("reparent: %v", err)
	}
	got, err := st.Get("moved")
	if err != nil || got.ParentID != "second" || got.Group != "g2" {
		t.Fatalf("reparented = %+v err %v", got, err)
	}
	kids, err := st.Children("second")
	if err != nil || len(kids) != 2 || kids[0].ID != "held" || kids[1].ID != "moved" {
		t.Fatalf("new siblings = %+v err %v", kids, err)
	}
	if kids, err := st.Children("first"); err != nil || len(kids) != 0 {
		t.Fatalf("old parent still holds %+v err %v", kids, err)
	}
}

func TestPlaceSessionRefusesParentThatHasChildren(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("host", "g1")); err != nil {
		t.Fatalf("host: %v", err)
	}
	child := sample("sh", "g1")
	child.ParentID = "host"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := st.PlaceSession("host", "g1", "agent"); err == nil {
		t.Fatal("nested a session that has children")
	}
	host, err := st.Get("host")
	if err != nil || host.ParentID != "" {
		t.Fatalf("host = %+v err %v", host, err)
	}
	got, err := st.Get("sh")
	if err != nil || got.ParentID != "host" || got.Group != "g1" {
		t.Fatalf("child = %+v err %v", got, err)
	}
}

func TestPlaceSessionMovesAgentChildren(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	child := sample("sh", "g1")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := st.PlaceSession("agent", "g2", ""); err != nil {
		t.Fatalf("move agent: %v", err)
	}
	agent, _ := st.Get("agent")
	sh, _ := st.Get("sh")
	if agent.Group != "g2" || sh.Group != "g2" || sh.ParentID != "agent" {
		t.Fatalf("agent=%+v child=%+v", agent, sh)
	}
}

func TestReorderSessionStaysInSiblingSet(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("other", "g")); err != nil {
		t.Fatalf("other: %v", err)
	}
	a := sample("a", "g")
	a.ParentID = "agent"
	b := sample("b", "g")
	b.ParentID = "agent"
	if err := st.CreateSession(a); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := st.CreateSession(b); err != nil {
		t.Fatalf("b: %v", err)
	}
	moved, err := st.ReorderSession("a", 1, false)
	if err != nil || !moved {
		t.Fatalf("reorder child: moved=%v err=%v", moved, err)
	}
	kids, _ := st.Children("agent")
	if len(kids) != 2 || kids[0].ID != "b" || kids[1].ID != "a" {
		t.Fatalf("child order %+v", kids)
	}
	roots := []string{}
	for _, id := range listIDs(t, st, false) {
		if id == "agent" || id == "other" {
			roots = append(roots, id)
		}
	}
	if len(roots) != 2 || roots[0] != "agent" || roots[1] != "other" {
		t.Fatalf("un-nested order %v", roots)
	}
	if err := st.SwapSessionOrder("agent", "a"); err == nil {
		t.Fatal("agent and its child are not siblings")
	}
}

func TestRemoveGroupMovesTheSubtreeInOneStroke(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("fleet", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.CreateGroup("fleet/backend", ""); err != nil {
		t.Fatalf("CreateGroup nested: %v", err)
	}
	if err := st.CreateSession(sample("aa", "fleet")); err != nil {
		t.Fatalf("create: %v", err)
	}
	nested := sample("bb", "fleet/backend")
	nested.ParentID = "aa"
	if err := st.CreateSession(nested); err != nil {
		t.Fatalf("create nested: %v", err)
	}

	removed, moved, err := st.RemoveGroup("fleet")
	if err != nil {
		t.Fatalf("RemoveGroup: %v", err)
	}
	sort.Strings(removed)
	if !slices.Equal(removed, []string{"fleet", "fleet/backend"}) {
		t.Fatalf("removed = %v", removed)
	}
	sort.Strings(moved)
	if !slices.Equal(moved, []string{"aa", "bb"}) {
		t.Fatalf("moved = %v", moved)
	}
	after, err := st.Get("bb")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Group != "" || after.ParentID != "aa" {
		t.Fatalf("nested session moved to %q under %q; want the root under aa", after.Group, after.ParentID)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	for _, g := range groups {
		if strings.HasPrefix(g.Name, "fleet") {
			t.Fatalf("group %q survived", g.Name)
		}
	}
}

func TestRemoveGroupReportsAMissingGroup(t *testing.T) {
	st := newTestStore(t)
	if _, _, err := st.RemoveGroup("ghost"); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("err = %v, want ErrGroupNotFound", err)
	}
	if _, _, err := st.RemoveGroup(""); err == nil {
		t.Fatal("removing the root group was accepted")
	}
}

// A group name may carry LIKE wildcards; they must not pull a sibling
// group's sessions into the move.
func TestRemoveGroupMatchesWildcardNamesLiterally(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("a_c", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.CreateGroup("abc", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := st.CreateSession(sample("cc", "abc/sub")); err != nil {
		t.Fatalf("create: %v", err)
	}
	removed, moved, err := st.RemoveGroup("a_c")
	if err != nil {
		t.Fatalf("RemoveGroup: %v", err)
	}
	if len(moved) != 0 {
		t.Fatalf("moved = %v, want nothing from the sibling group", moved)
	}
	if !slices.Equal(removed, []string{"a_c"}) {
		t.Fatalf("removed = %v", removed)
	}
}

// A row claimed by the manager that can see its pane is not this manager's
// to write, however recent its own listing is.
func TestUpdateStatusOnSocketWritesOnlyWhatThisServerOwns(t *testing.T) {
	st := newTestStore(t)
	const mine, theirs = "/tmp/mine/agentmgr", "/tmp/theirs/agentmgr"
	sess := Session{ID: "sess-1", Name: "one", Tool: "claude", Cwd: "/tmp", Status: "working"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}

	written, err := st.UpdateStatusOnSocket(sess.ID, "idle", mine)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("an unclaimed row is the polling manager's to write")
	}

	if err := st.SetTmuxSocket(sess.ID, theirs); err != nil {
		t.Fatal(err)
	}
	written, err = st.UpdateStatusOnSocket(sess.ID, "dead", mine)
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Fatal("a row claimed by another server must keep its status")
	}
	got, err := st.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "idle" {
		t.Fatalf("status = %q, want the claim to have held it at idle", got.Status)
	}

	written, err = st.UpdateStatusOnSocket(sess.ID, "dead", theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("the server holding the row writes it")
	}
}
