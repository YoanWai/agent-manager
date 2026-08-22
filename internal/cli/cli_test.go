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
	"github.com/YoanWai/agent-manager/internal/update"
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

func (f *fakeSessions) DeleteGroup(sessionID, path string) (sessioncmd.GroupRemoval, error) {
	f.callerID, f.groupPath = sessionID, path
	return sessioncmd.GroupRemoval{Removed: []string{path}, Moved: []string{"beef1234"}}, f.failWith
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
	closedID   string
	command    string
	keys       []string
	opts       sessioncmd.CreateTerminalOptions
	failWith   error
}

func (f *fakeTerminals) List(sessionID string) ([]sessioncmd.Terminal, error) {
	f.callerID = sessionID
	return []sessioncmd.Terminal{{ID: "t1", Name: "build", Directory: "/repo", Status: "idle", Running: true}}, f.failWith
}

func (f *fakeTerminals) Close(sessionID, terminalID string) error {
	f.callerID, f.closedID = sessionID, terminalID
	return f.failWith
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
		{"delete-group", []string{"api/web"}, func(out io.Writer, args []string) error { return runDeleteGroup(out, sessions, args, "cafe0001") }},
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
		{"terminal close", []string{"t1"}, func(out io.Writer, args []string) error { return runTerminalClose(out, terminals, args, "cafe0001") }},
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
	preamble, _, found := strings.Cut(Help("dev"), "\n"+sessionSection().title)
	if !found {
		t.Fatalf("help has no sections:\n%s", Help("dev"))
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
		{usageUpdate, func(out *bytes.Buffer, args []string) error {
			swapUpdateSeams(t, "agent-manager", update.Manager{},
				update.Result{Releases: []update.Release{{Version: "v0.31.0"}}}, nil)
			return runUpdate("0.31.0")(out, args, "", t.TempDir())
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
	table := Commands("dev")
	registered := []string{
		"sessions", "spawn", "send", "read", "wait", "message-status", "kill", "revive", "archive",
		"groups", "create-group", "delete-group", "task", "reserve", "release-files", "reservations", "terminal",
		"rename", "review-repo", "review-base", "review-mode", "review-comment", "update",
	}
	for _, name := range registered {
		if table[name] == nil {
			t.Fatalf("%s is not registered as a subcommand", name)
		}
	}
	if len(table) != len(registered) {
		t.Fatalf("the command table holds %d entries, not the %d named here: %v", len(table), len(registered), table)
	}

	help := Help("dev")
	for _, line := range []string{usageSessions, usageReserve, usageTerminalSend, usageRename, usageReviewComment, usageUpdate, "task <list|create|claim|finish|release|delete>"} {
		if !strings.Contains(help, line) {
			t.Fatalf("help is missing %q:\n%s", line, help)
		}
	}
}
