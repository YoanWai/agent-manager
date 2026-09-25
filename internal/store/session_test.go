package store

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

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

func TestLastPromptRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("a", "g1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.SetLastPrompt("a", "carry on with the plan"); err != nil {
		t.Fatalf("set last prompt: %v", err)
	}
	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastPrompt != "carry on with the plan" {
		t.Fatalf("last prompt = %q", got.LastPrompt)
	}
	listed, err := st.ListSessions(false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listed[0].LastPrompt != "carry on with the plan" {
		t.Fatalf("listed last prompt = %q", listed[0].LastPrompt)
	}
	if err := st.SetLastPrompt("missing", "x"); !errors.Is(err, ErrSessionGone) {
		t.Fatalf("want ErrSessionGone, got %v", err)
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

func TestRenameSessionWorktreeBranch(t *testing.T) {
	s := newTestStore(t)
	sess := Session{
		ID: "wt2", Name: "claude-7a72", Tool: "claude", Cwd: "/tmp/repo-worktrees/claude-7a72",
		WorktreeRepo: "/tmp/repo", WorktreeBranch: "am/claude-7a72",
	}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.RenameSessionWorktreeBranch("wt2", "am/renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := s.Get("wt2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Cwd != sess.Cwd || got.WorktreeBranch != "am/renamed" {
		t.Fatalf("rename did not land: %+v", got)
	}
	if got.WorktreeRepo != "/tmp/repo" {
		t.Fatalf("rename disturbed the repo root: %q", got.WorktreeRepo)
	}
	if err := s.RenameSessionWorktreeBranch("ghost", "am/x"); err == nil {
		t.Fatal("renaming an unknown session's branch should error")
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

// An empty store is a real pre-launch state: it must persist as {} and come
// back as a non-nil map, where nil clears the column for good.
func TestRelaunchSnapshotRoundTrip(t *testing.T) {
	st := newTestStore(t)
	sess := sample("sess", "g")
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.RelaunchSnapshot == nil || len(got.RelaunchSnapshot) != 0 {
		t.Fatalf("empty snapshot decoded to %v, want a non-nil empty map", got.RelaunchSnapshot)
	}
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{"conv-1": 123}); err != nil {
		t.Fatal(err)
	}
	got, err = st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.RelaunchSnapshot) != 1 || got.RelaunchSnapshot["conv-1"] != 123 {
		t.Fatalf("snapshot = %v, want conv-1 at 123", got.RelaunchSnapshot)
	}
	if err := st.SetRelaunchSnapshot("sess", nil); err != nil {
		t.Fatal(err)
	}
	got, err = st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.RelaunchSnapshot != nil {
		t.Fatalf("nil snapshot cleared to %v, want nil", got.RelaunchSnapshot)
	}
}

// Binding an id clears the snapshot in the same statement, so a relaunch
// landing between the two never loses its fresh snapshot.
func TestBindAgentSessionIDClearsTheSnapshot(t *testing.T) {
	st := newTestStore(t)
	sess := sample("sess", "g")
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	launch := time.Now()
	if err := st.SetAgentLaunchedAt("sess", launch); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{"conv-1": 123}); err != nil {
		t.Fatal(err)
	}
	bound, err := st.BindAgentSessionID("sess", "conv-1", launch)
	if err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Fatal("bind refused")
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "conv-1" || got.RelaunchSnapshot != nil {
		t.Fatalf("after bind: id=%q snapshot=%v, want conv-1 and nil", got.AgentSessionID, got.RelaunchSnapshot)
	}
}
