package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// fakeSessions records what a subcommand asked the layer for, so the tests
// exercise argument parsing and output without a tmux socket or a store.
type fakeSessions struct {
	callerID  string
	targetID  string
	message   string
	opts      sessioncmd.CreateSessionOptions
	until     []string
	timeout   time.Duration
	messageID int64
	archived  bool
	groupPath string
	directory string
	taskID    string
	title     string
	body      string
	dependsOn []string
	patterns  []string
	mode      string
	note      string
	ttl       time.Duration
	deleted   bool

	session   sessioncmd.Session
	wait      sessioncmd.WaitResult
	task      sessioncmd.Task
	reserve   sessioncmd.ReserveResult
	released  int
	failWith  error
	callCount int
}

func (f *fakeSessions) List(sessionID string) ([]sessioncmd.Session, error) {
	f.callerID = sessionID
	f.callCount++
	return []sessioncmd.Session{f.session}, f.failWith
}

func (f *fakeSessions) Create(sessionID string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error) {
	f.callerID, f.opts = sessionID, opts
	return f.session, f.failWith
}

func (f *fakeSessions) Send(sessionID, targetID, message string) (sessioncmd.SendResult, error) {
	f.callerID, f.targetID, f.message = sessionID, targetID, message
	return sessioncmd.SendResult{MessageID: 7, QueuePosition: 1, ManagerAwake: true}, f.failWith
}

func (f *fakeSessions) Read(sessionID, targetID string) (sessioncmd.SessionScreen, error) {
	f.callerID, f.targetID = sessionID, targetID
	return sessioncmd.SessionScreen{Session: f.session, Output: "screen text"}, f.failWith
}

func (f *fakeSessions) Wait(ctx context.Context, sessionID, targetID string, until []string, timeout time.Duration) (sessioncmd.WaitResult, error) {
	f.callerID, f.targetID, f.until, f.timeout = sessionID, targetID, until, timeout
	return f.wait, f.failWith
}

func (f *fakeSessions) MessageStatus(sessionID string, messageID int64) (sessioncmd.MessageState, error) {
	f.callerID, f.messageID = sessionID, messageID
	return sessioncmd.MessageState{MessageID: messageID, SessionID: "beef", State: "delivered"}, f.failWith
}

func (f *fakeSessions) Kill(sessionID, targetID string) (sessioncmd.Session, error) {
	f.callerID, f.targetID = sessionID, targetID
	return f.session, f.failWith
}

func (f *fakeSessions) Revive(sessionID, targetID string) (sessioncmd.Session, error) {
	f.callerID, f.targetID = sessionID, targetID
	return f.session, f.failWith
}

// The row an archive returns carries the state it landed in, which is what
// the front reads its verb off.
func (f *fakeSessions) Archive(sessionID, targetID string, archived bool) (sessioncmd.Session, error) {
	f.callerID, f.targetID, f.archived = sessionID, targetID, archived
	updated := f.session
	updated.Archived = archived
	return updated, f.failWith
}

func (f *fakeSessions) Groups(sessionID string) ([]sessioncmd.Group, error) {
	f.callerID = sessionID
	return []sessioncmd.Group{{Path: "api", Sessions: 2}}, f.failWith
}

func (f *fakeSessions) CreateGroup(sessionID, path, directory string) (sessioncmd.Group, error) {
	f.callerID, f.groupPath, f.directory = sessionID, path, directory
	return sessioncmd.Group{Path: path, Directory: directory}, f.failWith
}

func (f *fakeSessions) Tasks(sessionID string) ([]sessioncmd.Task, error) {
	f.callerID = sessionID
	return []sessioncmd.Task{f.task}, f.failWith
}

func (f *fakeSessions) CreateTask(sessionID, title, body string, dependsOn []string) (sessioncmd.Task, error) {
	f.callerID, f.title, f.body, f.dependsOn = sessionID, title, body, dependsOn
	return f.task, f.failWith
}

func (f *fakeSessions) ClaimTask(sessionID, taskID string) (sessioncmd.Task, error) {
	f.callerID, f.taskID = sessionID, taskID
	return f.task, f.failWith
}

func (f *fakeSessions) FinishTask(sessionID, taskID string) (sessioncmd.Task, error) {
	f.callerID, f.taskID = sessionID, taskID
	return f.task, f.failWith
}

func (f *fakeSessions) ReleaseTask(sessionID, taskID string) (sessioncmd.Task, error) {
	f.callerID, f.taskID = sessionID, taskID
	return f.task, f.failWith
}

func (f *fakeSessions) DeleteTask(sessionID, taskID string) error {
	f.callerID, f.taskID, f.deleted = sessionID, taskID, true
	return f.failWith
}

func (f *fakeSessions) Reserve(sessionID string, patterns []string, mode, note string, ttl time.Duration) (sessioncmd.ReserveResult, error) {
	f.callerID, f.patterns, f.mode, f.note, f.ttl = sessionID, patterns, mode, note, ttl
	return f.reserve, f.failWith
}

func (f *fakeSessions) ReleaseFiles(sessionID string, patterns []string) (int, error) {
	f.callerID, f.patterns = sessionID, patterns
	return f.released, f.failWith
}

func (f *fakeSessions) Reservations(sessionID string) ([]sessioncmd.Reservation, error) {
	f.callerID = sessionID
	return []sessioncmd.Reservation{{Pattern: "internal/cli", Mode: "exclusive", Holder: "api", ExpiresIn: "30m0s"}}, f.failWith
}

type fakeTerminals struct {
	callerID   string
	terminalID string
	command    string
	keys       []string
	opts       sessioncmd.CreateTerminalOptions
	failWith   error
}

func (f *fakeTerminals) List(sessionID string) ([]sessioncmd.Terminal, error) {
	f.callerID = sessionID
	return []sessioncmd.Terminal{{ID: "t1", Name: "build", Directory: "/repo", Status: "idle", Running: true}}, f.failWith
}

func (f *fakeTerminals) Create(sessionID string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	f.callerID, f.opts = sessionID, opts
	return sessioncmd.Terminal{ID: "t2", Name: "zsh-t2", Directory: "/repo"}, f.failWith
}

// The fake decides the input kind the way the real layer does, since the
// front is only allowed to report what it was handed.
func (f *fakeTerminals) Send(sessionID, terminalID, command string, keys []string) (sessioncmd.TerminalInput, error) {
	f.callerID, f.terminalID, f.command, f.keys = sessionID, terminalID, command, keys
	sent := "keys"
	if strings.TrimSpace(command) != "" {
		sent = "command"
	}
	return sessioncmd.TerminalInput{TerminalID: terminalID, Sent: sent}, f.failWith
}

func (f *fakeTerminals) Read(sessionID, terminalID string) (sessioncmd.TerminalScreen, error) {
	f.callerID, f.terminalID = sessionID, terminalID
	return sessioncmd.TerminalScreen{Output: "build passed"}, f.failWith
}

func sampleSession() sessioncmd.Session {
	return sessioncmd.Session{
		ID: "beef1234", Name: "api-worker", Tool: "claude",
		Group: "api", Directory: "/repo", Status: "working", Running: true,
	}
}

func TestSessionCommandsParseArgumentsAndPrintSentences(t *testing.T) {
	cases := []struct {
		name    string
		run     func(*bytes.Buffer, *fakeSessions, []string) error
		args    []string
		want    string
		inspect func(*testing.T, *fakeSessions)
	}{
		{
			name: "sessions lists every row",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runSessions(out, f, args, "cafe0001")
			},
			want: "- api-worker (beef1234) running claude in api at /repo; status=working; running=true",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.callerID != "cafe0001" {
					t.Fatalf("caller = %q", f.callerID)
				}
			},
		},
		{
			name: "send takes a target and a message",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runSend(out, f, args, "cafe0001")
			},
			args: []string{"beef1234", "ship it"},
			want: "queued message 7 for session beef1234 at position 1",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.targetID != "beef1234" || f.message != "ship it" {
					t.Fatalf("send got target %q message %q", f.targetID, f.message)
				}
			},
		},
		{
			name: "read prints the pane",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runRead(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "screen text",
		},
		{
			name: "kill names what it stopped",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runKill(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "killed api-worker (beef1234)",
		},
		{
			name: "revive names what it brought back",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runRevive(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "revived api-worker (beef1234)",
		},
		{
			name: "archive files a session away by default",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runArchive(out, f, args, "cafe0001")
			},
			args: []string{"beef1234"},
			want: "archived api-worker (beef1234)",
			inspect: func(t *testing.T, f *fakeSessions) {
				if !f.archived {
					t.Fatal("archive without --restore should archive")
				}
			},
		},
		{
			name: "archive --restore puts it back",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runArchive(out, f, args, "cafe0001")
			},
			args: []string{"beef1234", "--restore"},
			want: "restored api-worker (beef1234)",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.archived {
					t.Fatal("--restore should unarchive")
				}
			},
		},
		{
			name: "message-status reads the id back",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runMessageStatus(out, f, args, "cafe0001")
			},
			args: []string{"7"},
			want: "message 7 to session beef is delivered",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.messageID != 7 {
					t.Fatalf("message id = %d", f.messageID)
				}
			},
		},
		{
			name: "groups lists the tree",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runGroups(out, f, args, "cafe0001")
			},
			want: "- api; sessions=2",
		},
		{
			name: "create-group takes a path and a directory",
			run: func(out *bytes.Buffer, f *fakeSessions, args []string) error {
				return runCreateGroup(out, f, args, "cafe0001")
			},
			args: []string{"api/web", "--directory", "/repo"},
			want: "created group api/web",
			inspect: func(t *testing.T, f *fakeSessions) {
				if f.groupPath != "api/web" || f.directory != "/repo" {
					t.Fatalf("create-group got %q %q", f.groupPath, f.directory)
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			fake := &fakeSessions{session: sampleSession()}
			if err := testCase.run(out, fake, testCase.args); err != nil {
				t.Fatalf("run: %v", err)
			}
			if !strings.Contains(out.String(), testCase.want) {
				t.Fatalf("output = %q, want it to contain %q", out.String(), testCase.want)
			}
			if testCase.inspect != nil {
				testCase.inspect(t, fake)
			}
		})
	}
}

// A shell caller reads the exit status and the error line, so a handler
// that printed its sentence anyway would tell an agent the work happened.
// The MCP front pins the same contract for its tools.
func TestALayerFailureReachesTheCaller(t *testing.T) {
	failure := errors.New("session is not running")
	sessions := &fakeSessions{session: sampleSession(), failWith: failure}
	terminals := &fakeTerminals{failWith: failure}
	cases := []struct {
		name string
		args []string
		run  func(io.Writer, []string) error
	}{
		{"sessions", nil, func(out io.Writer, args []string) error { return runSessions(out, sessions, args, "cafe0001") }},
		{"spawn", nil, func(out io.Writer, args []string) error { return runSpawn(out, sessions, args, "cafe0001") }},
		{"send", []string{"beef1234", "ship it"}, func(out io.Writer, args []string) error { return runSend(out, sessions, args, "cafe0001") }},
		{"read", []string{"beef1234"}, func(out io.Writer, args []string) error { return runRead(out, sessions, args, "cafe0001") }},
		{"wait", []string{"beef1234"}, func(out io.Writer, args []string) error { return runWait(out, sessions, args, "cafe0001") }},
		{"message-status", []string{"7"}, func(out io.Writer, args []string) error { return runMessageStatus(out, sessions, args, "cafe0001") }},
		{"kill", []string{"beef1234"}, func(out io.Writer, args []string) error { return runKill(out, sessions, args, "cafe0001") }},
		{"revive", []string{"beef1234"}, func(out io.Writer, args []string) error { return runRevive(out, sessions, args, "cafe0001") }},
		{"archive", []string{"beef1234"}, func(out io.Writer, args []string) error { return runArchive(out, sessions, args, "cafe0001") }},
		{"groups", nil, func(out io.Writer, args []string) error { return runGroups(out, sessions, args, "cafe0001") }},
		{"create-group", []string{"api/web"}, func(out io.Writer, args []string) error { return runCreateGroup(out, sessions, args, "cafe0001") }},
		{"task list", nil, func(out io.Writer, args []string) error { return runTaskList(out, sessions, args, "cafe0001") }},
		{"task create", []string{"ship the api"}, func(out io.Writer, args []string) error { return runTaskCreate(out, sessions, args, "cafe0001") }},
		{"task claim", nil, func(out io.Writer, args []string) error { return runTaskClaim(out, sessions, args, "cafe0001") }},
		{"task finish", []string{"t1"}, func(out io.Writer, args []string) error { return runTaskFinish(out, sessions, args, "cafe0001") }},
		{"task release", []string{"t1"}, func(out io.Writer, args []string) error { return runTaskRelease(out, sessions, args, "cafe0001") }},
		{"task delete", []string{"t1"}, func(out io.Writer, args []string) error { return runTaskDelete(out, sessions, args, "cafe0001") }},
		{"reserve", []string{"internal/cli"}, func(out io.Writer, args []string) error { return runReserve(out, sessions, args, "cafe0001") }},
		{"release-files", nil, func(out io.Writer, args []string) error { return runReleaseFiles(out, sessions, args, "cafe0001") }},
		{"reservations", nil, func(out io.Writer, args []string) error { return runReservations(out, sessions, args, "cafe0001") }},
		{"terminal list", nil, func(out io.Writer, args []string) error { return runTerminalList(out, terminals, args, "cafe0001") }},
		{"terminal create", nil, func(out io.Writer, args []string) error { return runTerminalCreate(out, terminals, args, "cafe0001") }},
		{"terminal send", []string{"t1"}, func(out io.Writer, args []string) error { return runTerminalSend(out, terminals, args, "cafe0001") }},
		{"terminal read", []string{"t1"}, func(out io.Writer, args []string) error { return runTerminalRead(out, terminals, args, "cafe0001") }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			err := testCase.run(out, testCase.args)
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want the layer's own", err)
			}
			if out.Len() != 0 {
				t.Fatalf("a failed call printed a result anyway: %q", out.String())
			}
		})
	}
}

// Only a flag the caller typed may reach the layer: a zero value passed
// anyway would silently override the group and worktree a spawn inherits.
func TestSpawnPassesOnlyTheFlagsGiven(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeSessions{session: sampleSession()}
	args := []string{"--name", "api-worker", "--prompt", "build the api", "--tool", "claude", "--directory", "/repo"}
	if err := runSpawn(out, fake, args, "cafe0001"); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if fake.opts.Name != "api-worker" || fake.opts.Prompt != "build the api" || fake.opts.Tool != "claude" || fake.opts.Directory != "/repo" {
		t.Fatalf("spawn opts = %+v", fake.opts)
	}
	if fake.opts.Group != nil || fake.opts.Worktree != nil {
		t.Fatalf("untyped flags should stay inherited, got group=%v worktree=%v", fake.opts.Group, fake.opts.Worktree)
	}
	if !strings.HasPrefix(out.String(), "created api-worker (beef1234)") {
		t.Fatalf("spawn output = %q", out.String())
	}

	explicit := &fakeSessions{session: sampleSession()}
	if err := runSpawn(&bytes.Buffer{}, explicit, []string{"--group", "", "--worktree"}, "cafe0001"); err != nil {
		t.Fatalf("spawn explicit: %v", err)
	}
	if explicit.opts.Group == nil || *explicit.opts.Group != "" {
		t.Fatalf("an explicit empty group targets the root, got %v", explicit.opts.Group)
	}
	if explicit.opts.Worktree == nil || !*explicit.opts.Worktree {
		t.Fatalf("--worktree should be passed on, got %v", explicit.opts.Worktree)
	}
}

func TestWaitCollectsStatesAndFailsOnTimeout(t *testing.T) {
	reached := &fakeSessions{wait: sessioncmd.WaitResult{
		Session: sampleSession(), Reached: true, Outcome: sessioncmd.WaitReached, Waited: "3s", ManagerAwake: true,
	}}
	out := &bytes.Buffer{}
	if err := runWait(out, reached, []string{"beef1234", "--until", "idle,finished", "--timeout", "10s"}, "cafe0001"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(reached.until) != 2 || reached.until[0] != "idle" || reached.until[1] != "finished" {
		t.Fatalf("until = %v", reached.until)
	}
	if reached.timeout != 10*time.Second {
		t.Fatalf("timeout = %s", reached.timeout)
	}
	if !strings.Contains(out.String(), "is working after 3s") {
		t.Fatalf("wait output = %q", out.String())
	}

	timedOut := &fakeSessions{wait: sessioncmd.WaitResult{
		Session: sampleSession(), Outcome: sessioncmd.WaitTimedOut, Waited: "50s", ManagerAwake: true,
	}}
	err := runWait(&bytes.Buffer{}, timedOut, []string{"beef1234"}, "cafe0001")
	if err == nil || !strings.HasPrefix(err.Error(), "timed out:") {
		t.Fatalf("a timeout should fail the command, got %v", err)
	}
}

func TestJSONFlagPrintsTheRecord(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeSessions{session: sampleSession()}
	if err := runSessions(out, fake, []string{"--json"}, "cafe0001"); err != nil {
		t.Fatalf("sessions --json: %v", err)
	}
	var listed []sessioncmd.Session
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("sessions --json is not JSON: %v (%q)", err, out.String())
	}
	if len(listed) != 1 || listed[0].ID != "beef1234" || listed[0].Tool != "claude" {
		t.Fatalf("sessions --json = %+v", listed)
	}

	released := &bytes.Buffer{}
	if err := runReleaseFiles(released, &fakeSessions{released: 3}, []string{"--json"}, "cafe0001"); err != nil {
		t.Fatalf("release-files --json: %v", err)
	}
	var count releasedCount
	if err := json.Unmarshal(released.Bytes(), &count); err != nil {
		t.Fatalf("release-files --json is not JSON: %v (%q)", err, released.String())
	}
	if count.Released != 3 {
		t.Fatalf("release-files --json = %+v", count)
	}
}

func TestMissingSessionIDIsAUsageError(t *testing.T) {
	for name, run := range map[string]func(*bytes.Buffer, *fakeSessions, []string) error{
		"read": func(out *bytes.Buffer, f *fakeSessions, args []string) error {
			return runRead(out, f, args, "cafe0001")
		},
		"kill": func(out *bytes.Buffer, f *fakeSessions, args []string) error {
			return runKill(out, f, args, "cafe0001")
		},
		"revive": func(out *bytes.Buffer, f *fakeSessions, args []string) error {
			return runRevive(out, f, args, "cafe0001")
		},
		"archive": func(out *bytes.Buffer, f *fakeSessions, args []string) error {
			return runArchive(out, f, args, "cafe0001")
		},
		"wait": func(out *bytes.Buffer, f *fakeSessions, args []string) error {
			return runWait(out, f, args, "cafe0001")
		},
		"send": func(out *bytes.Buffer, f *fakeSessions, args []string) error {
			return runSend(out, f, args, "cafe0001")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeSessions{session: sampleSession()}
			err := run(&bytes.Buffer{}, fake, nil)
			if err == nil {
				t.Fatal("a command with no session id should not reach the layer")
			}
			if !strings.HasPrefix(err.Error(), "usage: agent-manager "+name) {
				t.Fatalf("error = %q, want the usage line", err)
			}
			if fake.callerID != "" {
				t.Fatal("the layer was called despite the missing id")
			}
		})
	}
}

func TestMessageStatusRefusesANonNumericID(t *testing.T) {
	err := runMessageStatus(&bytes.Buffer{}, &fakeSessions{}, []string{"seven"}, "cafe0001")
	if err == nil || !strings.Contains(err.Error(), "agent-manager send prints the id it queued") {
		t.Fatalf("error = %v, want it to say where the id comes from", err)
	}
}

func TestHelpFlagPrintsUsageAndStops(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeSessions{session: sampleSession()}
	err := runSessions(out, fake, []string{"-h"}, "cafe0001")
	if !errors.Is(err, ErrUsageShown) {
		t.Fatalf("-h should return ErrUsageShown, got %v", err)
	}
	if !strings.Contains(out.String(), "usage: agent-manager "+usageSessions) {
		t.Fatalf("-h output = %q", out.String())
	}
	if fake.callCount != 0 {
		t.Fatal("-h should not reach the layer")
	}

	group := &bytes.Buffer{}
	if err := dispatch(group, "task", taskVerbs(), []string{"-h"}, "cafe0001", t.TempDir()); !errors.Is(err, ErrUsageShown) {
		t.Fatalf("task -h should return ErrUsageShown, got %v", err)
	}
	if !strings.Contains(group.String(), usageTaskClaim) {
		t.Fatalf("task -h output = %q", group.String())
	}
}

func TestTaskVerbsDispatch(t *testing.T) {
	out := &bytes.Buffer{}
	fake := &fakeSessions{task: sessioncmd.Task{ID: "t1", Title: "wire the cli", State: "in_progress", OwnerName: "api-worker"}}

	if err := runTaskList(out, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("task list: %v", err)
	}
	if !strings.Contains(out.String(), "- wire the cli (t1) [in_progress] held by api-worker") {
		t.Fatalf("task list output = %q", out.String())
	}

	created := &bytes.Buffer{}
	if err := runTaskCreate(created, fake, []string{"wire the cli", "--body", "five files", "--depends-on", "a1,b2"}, "cafe0001"); err != nil {
		t.Fatalf("task create: %v", err)
	}
	if fake.title != "wire the cli" || fake.body != "five files" {
		t.Fatalf("task create got %q / %q", fake.title, fake.body)
	}
	if len(fake.dependsOn) != 2 || fake.dependsOn[0] != "a1" || fake.dependsOn[1] != "b2" {
		t.Fatalf("depends-on = %v", fake.dependsOn)
	}

	if err := runTaskClaim(&bytes.Buffer{}, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("task claim: %v", err)
	}
	if fake.taskID != "" {
		t.Fatalf("a bare claim takes the next task, got id %q", fake.taskID)
	}
	if err := runTaskClaim(&bytes.Buffer{}, fake, []string{"t1"}, "cafe0001"); err != nil {
		t.Fatalf("task claim t1: %v", err)
	}
	if fake.taskID != "t1" {
		t.Fatalf("claim id = %q", fake.taskID)
	}

	settled := map[string]func(io.Writer, taskCommands, []string, string) error{
		"finished": runTaskFinish,
		"released": runTaskRelease,
	}
	for verb, run := range settled {
		buf := &bytes.Buffer{}
		if err := run(buf, fake, []string{"t1"}, "cafe0001"); err != nil {
			t.Fatalf("task %s: %v", verb, err)
		}
		if !strings.HasPrefix(buf.String(), verb+" wire the cli (t1)") {
			t.Fatalf("task %s output = %q", verb, buf.String())
		}
	}

	deleted := &bytes.Buffer{}
	if err := runTaskDelete(deleted, fake, []string{"t1"}, "cafe0001"); err != nil {
		t.Fatalf("task delete: %v", err)
	}
	if !fake.deleted || deleted.String() != "deleted task t1\n" {
		t.Fatalf("task delete output = %q", deleted.String())
	}
}

func TestTaskGroupRejectsAnUnknownVerb(t *testing.T) {
	err := dispatch(&bytes.Buffer{}, "task", taskVerbs(), []string{"finnish", "t1"}, "cafe0001", t.TempDir())
	if err == nil {
		t.Fatal("an unknown verb should not dispatch")
	}
	want := `task has no "finnish" command; it takes list, create, claim, finish, release, delete`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}

	bare := dispatch(&bytes.Buffer{}, "task", taskVerbs(), nil, "cafe0001", t.TempDir())
	if bare == nil || bare.Error() != "usage: agent-manager task <list|create|claim|finish|release|delete>" {
		t.Fatalf("a bare group should print its verbs, got %v", bare)
	}
}

func TestReserveParsesPathsAndFlags(t *testing.T) {
	fake := &fakeSessions{reserve: sessioncmd.ReserveResult{
		Reserved: []sessioncmd.Reservation{{Pattern: "internal/cli", Mode: "exclusive"}},
	}}
	out := &bytes.Buffer{}
	args := []string{"internal/cli", "internal/ui", "--mode", "shared", "--note", "wiring", "--ttl", "45m"}
	if err := runReserve(out, fake, args, "cafe0001"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if len(fake.patterns) != 2 || fake.patterns[1] != "internal/ui" {
		t.Fatalf("patterns = %v", fake.patterns)
	}
	if fake.mode != "shared" || fake.note != "wiring" || fake.ttl != 45*time.Minute {
		t.Fatalf("reserve flags = %q %q %s", fake.mode, fake.note, fake.ttl)
	}
	if !strings.Contains(out.String(), "reserved internal/cli") {
		t.Fatalf("reserve output = %q", out.String())
	}

	if err := runReserve(&bytes.Buffer{}, fake, []string{"--", "-notaflag"}, "cafe0001"); err == nil {
		t.Fatal("a path that reads as a flag should be refused")
	}

	// The flags parse wherever they sit, so a leading dash is the whole
	// reason a mistyped one cannot be leased, and the refusal has to say so
	// rather than send the agent to reorder what it typed.
	mistyped := runReserve(&bytes.Buffer{}, fake, []string{"internal/cli", "--mod", "shared"}, "cafe0001")
	if mistyped == nil {
		t.Fatal("a mistyped flag should not be leased as a pattern")
	}
	if !strings.Contains(mistyped.Error(), `"--mod" starts with a dash`) ||
		!strings.Contains(mistyped.Error(), "usage: agent-manager reserve") {
		t.Fatalf("the refusal does not name the cause and the usage: %q", mistyped)
	}
	if err := runReserve(&bytes.Buffer{}, fake, nil, "cafe0001"); err == nil {
		t.Fatal("reserve needs at least one path")
	}
}

func TestReservationsAndReleaseReadTheSameLeases(t *testing.T) {
	out := &bytes.Buffer{}
	if err := runReservations(out, &fakeSessions{}, nil, "cafe0001"); err != nil {
		t.Fatalf("reservations: %v", err)
	}
	if !strings.Contains(out.String(), "- internal/cli (exclusive) held by api for 30m0s") {
		t.Fatalf("reservations output = %q", out.String())
	}

	released := &bytes.Buffer{}
	fake := &fakeSessions{released: 2}
	if err := runReleaseFiles(released, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("release-files: %v", err)
	}
	if len(fake.patterns) != 0 {
		t.Fatalf("naming no path releases everything, got %v", fake.patterns)
	}
	if released.String() != "released 2 reservation(s)\n" {
		t.Fatalf("release-files output = %q", released.String())
	}
}

func TestTerminalVerbsDispatch(t *testing.T) {
	fake := &fakeTerminals{}
	out := &bytes.Buffer{}
	if err := runTerminalList(out, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("terminal list: %v", err)
	}
	if !strings.Contains(out.String(), "- build (t1) in root at /repo; status=idle; running=true") {
		t.Fatalf("terminal list output = %q", out.String())
	}

	created := &bytes.Buffer{}
	if err := runTerminalCreate(created, fake, []string{"--directory", "/repo"}, "cafe0001"); err != nil {
		t.Fatalf("terminal create: %v", err)
	}
	if fake.opts.Directory != "/repo" || fake.opts.Group != nil {
		t.Fatalf("terminal create opts = %+v", fake.opts)
	}

	sent := &bytes.Buffer{}
	if err := runTerminalSend(sent, fake, []string{"t1", "--command", "go test ./..."}, "cafe0001"); err != nil {
		t.Fatalf("terminal send: %v", err)
	}
	if fake.command != "go test ./..." || sent.String() != "sent command to terminal t1\n" {
		t.Fatalf("terminal send got %q, printed %q", fake.command, sent.String())
	}

	keyed := &bytes.Buffer{}
	if err := runTerminalSend(keyed, fake, []string{"t1", "--keys", "C-c,Up"}, "cafe0001"); err != nil {
		t.Fatalf("terminal send keys: %v", err)
	}
	if len(fake.keys) != 2 || fake.keys[0] != "C-c" || keyed.String() != "sent keys to terminal t1\n" {
		t.Fatalf("terminal send keys = %v, printed %q", fake.keys, keyed.String())
	}

	read := &bytes.Buffer{}
	if err := runTerminalRead(read, fake, []string{"t1"}, "cafe0001"); err != nil {
		t.Fatalf("terminal read: %v", err)
	}
	if read.String() != "build passed\n" {
		t.Fatalf("terminal read output = %q", read.String())
	}

	if err := dispatch(&bytes.Buffer{}, "terminal", terminalVerbs(), []string{"tail"}, "cafe0001", t.TempDir()); err == nil {
		t.Fatal("an unknown terminal verb should not dispatch")
	}
}

// A bulleted or dash-leading line is ordinary agent prose, and the MCP
// front takes it without ceremony. Reading it as a flag makes the shell
// front refuse text the other front accepts, over a "--" escape that
// appears in no usage line.
func TestOperandTextMayStartWithADash(t *testing.T) {
	sent := &fakeSessions{session: sampleSession()}
	if err := runSend(&bytes.Buffer{}, sent, []string{"beef1234", "- fix the parser"}, "cafe0001"); err != nil {
		t.Fatalf("send with a dash-leading message: %v", err)
	}
	if sent.message != "- fix the parser" {
		t.Fatalf("message = %q", sent.message)
	}

	created := &fakeSessions{task: sessioncmd.Task{ID: "t1", Title: "-n flag handling", State: "pending"}}
	args := []string{"-n flag handling", "--body", "- start with the parser", "--json"}
	out := &bytes.Buffer{}
	if err := runTaskCreate(out, created, args, "cafe0001"); err != nil {
		t.Fatalf("task create with a dash-leading title: %v", err)
	}
	if created.title != "-n flag handling" || created.body != "- start with the parser" {
		t.Fatalf("create got title %q body %q", created.title, created.body)
	}
	// The flags either side of that text still have to be read as flags.
	if !json.Valid(out.Bytes()) {
		t.Fatalf("--json after a dash-leading operand was swallowed: %q", out.String())
	}

	// A flag the command does not define is still an operand, so the arity
	// check refuses it with the usage line rather than accepting it.
	if err := runSessions(&bytes.Buffer{}, &fakeSessions{}, []string{"--jsonn"}, "cafe0001"); err == nil {
		t.Fatal("an unknown flag should not be accepted as a session operand")
	}
}

// Help must not promise a flag a command refuses: an agent that believes a
// blanket promise gets "flag provided but not defined" instead of a record.
func TestJSONIsPromisedOnlyWhereTheCommandTakesIt(t *testing.T) {
	preamble, _, found := strings.Cut(Help(), "\n"+sessionSection().title)
	if !found {
		t.Fatalf("help has no sections:\n%s", Help())
	}
	if strings.Contains(preamble, "--json") {
		t.Fatalf("the preamble promises --json for every command, and several refuse it:\n%s", preamble)
	}

	cases := []struct {
		usage string
		run   func(*bytes.Buffer, []string) error
	}{
		{usageSessions, func(out *bytes.Buffer, args []string) error {
			return runSessions(out, &fakeSessions{session: sampleSession()}, args, "cafe0001")
		}},
		{usageTaskDelete, func(out *bytes.Buffer, args []string) error {
			return runTaskDelete(out, &fakeSessions{}, append([]string{"t1"}, args...), "cafe0001")
		}},
		{usageTerminalSend, func(out *bytes.Buffer, args []string) error {
			return runTerminalSend(out, &fakeTerminals{}, append([]string{"t1", "--keys", "C-c"}, args...), "cafe0001")
		}},
		{usageRename, func(out *bytes.Buffer, args []string) error {
			return runRename(out, append([]string{"payments-fix"}, args...), "cafe0001", t.TempDir())
		}},
		{usageReviewMode, func(out *bytes.Buffer, args []string) error {
			return runReviewMode(out, append([]string{"staged"}, args...), "cafe0001", t.TempDir())
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.usage, func(t *testing.T) {
			err := testCase.run(&bytes.Buffer{}, []string{"--json"})
			if advertised := strings.Contains(testCase.usage, "--json"); advertised != (err == nil) {
				t.Fatalf("usage %q disagrees with what --json did: %v", testCase.usage, err)
			}
		})
	}
}

// Which input kind went into a terminal, and whether an archive archived or
// restored, are the layer's answers. A front that reads them back off its
// own arguments drifts from the other front the moment either changes.
func TestAFrontReportsWhatTheLayerDecided(t *testing.T) {
	// A blank command is no command, so the layer sends the keys; the front
	// saw a --command flag either way.
	sent := &bytes.Buffer{}
	if err := runTerminalSend(sent, &fakeTerminals{}, []string{"t1", "--command", "   ", "--keys", "C-c"}, "cafe0001"); err != nil {
		t.Fatalf("terminal send: %v", err)
	}
	if sent.String() != "sent keys to terminal t1\n" {
		t.Fatalf("terminal send output = %q", sent.String())
	}

	archived := &bytes.Buffer{}
	if err := runArchive(archived, &fakeSessions{session: sampleSession()}, []string{"beef1234", "--restore"}, "cafe0001"); err != nil {
		t.Fatalf("archive --restore: %v", err)
	}
	if !strings.HasPrefix(archived.String(), "restored ") {
		t.Fatalf("archive --restore output = %q", archived.String())
	}
}

func TestCommandsAndHelpCoverEverySection(t *testing.T) {
	table := Commands()
	for _, name := range []string{
		"sessions", "spawn", "send", "read", "wait", "message-status", "kill", "revive", "archive",
		"groups", "create-group", "task", "reserve", "release-files", "reservations", "terminal",
		"rename", "review-repo", "review-base", "review-mode",
	} {
		if table[name] == nil {
			t.Fatalf("%s is not registered as a subcommand", name)
		}
	}

	help := Help()
	for _, line := range []string{usageSessions, usageReserve, usageTerminalSend, usageRename, "task <list|create|claim|finish|release|delete>"} {
		if !strings.Contains(help, line) {
			t.Fatalf("help is missing %q:\n%s", line, help)
		}
	}
}
