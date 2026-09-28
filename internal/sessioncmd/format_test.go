package sessioncmd

import "testing"

func assertFormatted(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("formatted text = %q, want %q", got, want)
	}
}

func TestFormatTerminals(t *testing.T) {
	root := Terminal{Name: "shell", ID: "term1", Directory: "/workspace", Status: "idle", Running: true}
	grouped := Terminal{Name: "logs", ID: "term2", Group: "backend", Directory: "/logs", Status: "dead"}

	assertFormatted(t, FormatTerminal(root), "shell (id term1) in root at /workspace")
	assertFormatted(t, FormatTerminalList(nil), "no managed terminals")
	assertFormatted(t, FormatTerminalList([]Terminal{root, grouped}),
		"- shell (id term1) in root at /workspace; status=idle; running=true\n"+
			"- logs (id term2) in backend at /logs; status=dead; running=false")
	assertFormatted(t, FormatTerminalInput(TerminalInput{TerminalID: "term1", Sent: "keys"}), "sent keys to terminal term1")
	assertFormatted(t, FormatTerminalScreen(TerminalScreen{}), "terminal screen is empty")
	assertFormatted(t, FormatTerminalScreen(TerminalScreen{Output: "$ pwd\n/workspace"}), "$ pwd\n/workspace")
}

func TestFormatSessions(t *testing.T) {
	root := Session{Name: "lead", ID: "abc123", Tool: "codex", Directory: "/repo", Status: "working", Running: true, Self: true}
	worker := Session{
		Name: "worker", ID: "def456", Tool: "claude", Group: "backend", Directory: "/worktree",
		Status: "waiting", Running: true, Archived: true, Branch: "feature",
	}

	assertFormatted(t, FormatSession(root), "lead (id abc123) running codex in root at /repo")
	assertFormatted(t, FormatSession(worker), "worker (id def456) running claude in backend at /worktree on branch feature")
	assertFormatted(t, FormatSessionList(nil), "no agent sessions")
	assertFormatted(t, FormatSessionList([]Session{root, worker}),
		"- lead (id abc123) running codex in root at /repo; status=working; running=true; this session\n"+
			"- worker (id def456) running claude in backend at /worktree on branch feature; status=waiting; running=true; archived")
	assertFormatted(t, FormatArchiveState(worker), "archived "+FormatSession(worker))
	restored := worker
	restored.Archived = false
	assertFormatted(t, FormatArchiveState(restored), "restored "+FormatSession(restored))
	assertFormatted(t, FormatSessionScreen(SessionScreen{}), "session screen is empty")
	assertFormatted(t, FormatSessionScreen(SessionScreen{Output: "finished"}), "finished")
}

func TestFormatSessionResults(t *testing.T) {
	session := Session{Name: "worker", ID: "abc123", Tool: "codex", Directory: "/repo", Status: "idle"}
	formatted := FormatSession(session) + " is idle after 2s"

	assertFormatted(t, FormatSendResult(SendResult{MessageID: 7, QueuePosition: 2, ManagerAwake: true}, "def456"),
		"queued message 7 for session def456 at position 2")
	assertFormatted(t, FormatSendResult(SendResult{MessageID: 8, QueuePosition: 1}, "def456"),
		"queued message 8 for session def456 at position 1; Agent Manager is not running, so it waits until the user opens it")
	assertFormatted(t, FormatMessageState(MessageState{MessageID: 7, SessionID: "def456", State: "queued"}),
		"message 7 to session def456 is queued")
	assertFormatted(t, FormatMessageState(MessageState{
		MessageID: 7, SessionID: "def456", State: "delivered", DeliveredAt: "2026-09-28T12:00:00Z",
	}), "message 7 to session def456 is delivered (delivered 2026-09-28T12:00:00Z)")
	assertFormatted(t, FormatMessageState(MessageState{
		MessageID: 8, SessionID: "def456", State: "held", Reason: "approval dialog",
	}), "message 8 to session def456 is held: approval dialog")

	dead := session
	dead.Status = "dead"
	for _, tc := range []struct {
		name   string
		result WaitResult
		want   string
	}{
		{name: "reached", result: WaitResult{Session: session, Outcome: WaitReached, Waited: "2s", ManagerAwake: true}, want: formatted},
		{name: "timed out", result: WaitResult{Session: session, Outcome: WaitTimedOut, Waited: "2s", ManagerAwake: true}, want: "timed out: " + formatted},
		{
			name:   "died while manager is closed",
			result: WaitResult{Session: dead, Outcome: WaitDied, Waited: "2s"},
			want: "the session died before reaching any awaited state: " + FormatSession(dead) + " is dead after 2s" +
				"; Agent Manager is not running, so this status is the last one it recorded",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertFormatted(t, FormatWaitResult(tc.result), tc.want)
		})
	}
}

func TestFormatTasks(t *testing.T) {
	owned := Task{Title: "add tests", ID: "task1", State: "in_progress", OwnerName: "worker", Mine: true}
	blocked := Task{Title: "ship release", ID: "task2", State: "pending", Blocked: true, BlockedBy: []string{"task1", "task0"}}

	assertFormatted(t, FormatTask(owned), "add tests (task1) [in_progress] held by worker")
	assertFormatted(t, FormatTask(blocked), "ship release (task2) [pending] blocked on task1, task0")
	assertFormatted(t, FormatTaskList(nil), "no tasks on the shared list")
	assertFormatted(t, FormatTaskList([]Task{owned, blocked}),
		"- add tests (task1) [in_progress] held by worker; yours\n"+
			"- ship release (task2) [pending] blocked on task1, task0")
}

func TestFormatReservations(t *testing.T) {
	mine := Reservation{Pattern: "internal/**", Mode: "exclusive", Holder: "worker", ExpiresIn: "30m0s", Note: "add coverage", Mine: true}
	other := Reservation{Pattern: "README.md", Mode: "shared", Holder: "lead", ExpiresIn: "10m0s"}

	assertFormatted(t, FormatReservation(mine), "internal/** (exclusive) held by worker for 30m0s: add coverage")
	assertFormatted(t, FormatReservations(nil), "no files are reserved")
	assertFormatted(t, FormatReservations([]Reservation{mine, other}),
		"- internal/** (exclusive) held by worker for 30m0s: add coverage; yours\n"+
			"- README.md (shared) held by lead for 10m0s")
	assertFormatted(t, FormatReserveResult(ReserveResult{Reserved: []Reservation{mine}}), "reserved internal/**")
	assertFormatted(t, FormatReserveResult(ReserveResult{Reserved: []Reservation{mine}, Conflicts: []Reservation{other}}),
		"reserved internal/**\nconflicts with leases already held; message the holder before editing:\n"+
			"- README.md (shared) held by lead for 10m0s")
	assertFormatted(t, FormatReleased(2), "released 2 reservation(s)")
}

func TestFormatGroups(t *testing.T) {
	assertFormatted(t, FormatGroupList(nil), "no groups; sessions live in the root")
	assertFormatted(t, FormatGroupList([]Group{
		{Path: "backend", Directory: "/repo/backend", Worktree: "on", Archived: true, Sessions: 2},
		{Path: "frontend", Sessions: 1},
	}), "- backend; sessions=2; directory=/repo/backend; worktree=on; archived\n- frontend; sessions=1")
	assertFormatted(t, FormatGroupRemoval(GroupRemoval{Removed: []string{"backend"}}), "deleted backend")
	assertFormatted(t, FormatGroupRemoval(GroupRemoval{
		Removed: []string{"backend", "backend/api"},
		Moved:   []string{"abc123", "def456"},
	}), "deleted backend, backend/api; 2 session(s) moved to the root group: abc123, def456")
}
