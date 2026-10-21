package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type exitStatus int

func (e exitStatus) Error() string { return "exit status " + strconv.Itoa(int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

type hostCall struct {
	destination string
	words       []string
}

// fakeHosts stands in for ssh: it decodes the remote command back into the
// agent-manager argv each host would run, and answers per host.
type fakeHosts struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []hostCall
	answer func(destination string, words []string) ([]byte, []byte, error)
}

func (f *fakeHosts) run(_ context.Context, argv []string) ([]byte, []byte, error) {
	words, err := remotetest.Words(argv)
	if err != nil || words[0] != "agent-manager" {
		f.t.Fatalf("remote command %q: %v", argv, err)
	}
	call := hostCall{destination: argv[len(argv)-2], words: words[1:]}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	answer := f.answer
	f.mu.Unlock()
	stdout, stderr, err := answer(call.destination, call.words)
	return remotetest.Answer(stdout), stderr, err
}

func (f *fakeHosts) recorded() []hostCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeHosts) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func gpuSnapshot() sessioncmd.Snapshot {
	return sessioncmd.Snapshot{
		Version:      sessioncmd.SnapshotVersion,
		ManagerAwake: true,
		Sessions: []sessioncmd.Session{{
			ID: "r1", Name: "remote-worker", Tool: "claude", Group: "work", Directory: "/home/me", Status: "idle", Running: true,
		}},
		Terminals: []sessioncmd.Terminal{
			{ID: "t1", Name: "shell", Group: "work", Directory: "/home/me", Running: true, ParentID: "r1", ParentName: "remote-worker"},
			{ID: "t2", Name: "old-shell", Archived: true},
		},
		Groups: []sessioncmd.Group{{Path: "work", Directory: "/home/me/work", Sessions: 1}},
	}
}

type remoteFixture struct {
	session     *mcp.ClientSession
	hosts       *fakeHosts
	local       *fakeSessionCommands
	terminals   *fakeTerminalCommands
	connections []store.Connection
	reads       int
	profile     string
	withStore   func(func(*store.Store) error) error
}

func newRemoteFixture(t *testing.T, sessionID string, answer func(destination string, words []string) ([]byte, []byte, error)) *remoteFixture {
	t.Helper()
	f := &remoteFixture{
		hosts: &fakeHosts{t: t, answer: answer},
		local: &fakeSessionCommands{listed: []sessioncmd.Session{{
			ID: "cafe", Name: "lead", Tool: "claude", Directory: "/work", Status: "working", Running: true, Self: true,
		}}},
		terminals:   &fakeTerminalCommands{listed: []sessioncmd.Terminal{{ID: "beef", Name: "local-shell", Running: true}}},
		connections: []store.Connection{{Name: "gpu", Destination: "me@gpu"}},
	}
	f.profile = t.TempDir()
	t.Cleanup(func() { os.RemoveAll(remote.ControlDir(f.profile)) })
	f.withStore = storeAt(f.profile)
	f.session = f.connect(t, sessionID)
	return f
}

// connect serves another session from the same machine: the same store,
// connections and hosts.
func (f *remoteFixture) connect(t *testing.T, sessionID string) *mcp.ClientSession {
	t.Helper()
	source := remoteSource{
		client: remote.New(f.profile, remote.WithRunner(f.hosts.run)),
		connections: func() ([]store.Connection, error) {
			f.reads++
			return slices.Clone(f.connections), nil
		},
		withStore: f.withStore,
	}
	return connectServer(t, newServerWithMailbox(sessionID, "test", true, f.terminals, f.local, &fakeReporter{}, sessioncmd.NewMailbox(f.profile), source))
}

func (f *remoteFixture) opener(t *testing.T, host, terminalID string) string {
	t.Helper()
	var opener string
	if err := f.withStore(func(st *store.Store) error {
		var err error
		opener, err = st.RemoteTerminalOpener(host, terminalID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return opener
}

func structured(t *testing.T, result *mcp.CallToolResult, into any) {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool error: %+v", result.Content)
	}
	if err := json.Unmarshal(mustJSON(t, result.StructuredContent), into); err != nil {
		t.Fatal(err)
	}
}

func resultText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String()
}

func TestListingsAppendEachConnectionWithQualifiedIDs(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(destination string, words []string) ([]byte, []byte, error) {
		if destination == "me@cpu" {
			return nil, []byte("ssh: connect to host cpu port 22: Connection refused"), exitStatus(255)
		}
		return mustJSON(t, gpuSnapshot()), nil, nil
	})
	f.connections = append(f.connections, store.Connection{Name: "cpu", Destination: "me@cpu"})

	result := callTool(t, f.session, "list_sessions", map[string]any{})
	var sessions listSessionsOutput
	structured(t, result, &sessions)
	want := []sessionRow{
		{Session: f.local.listed[0]},
		{Session: sessioncmd.Session{ID: "gpu::r1", Name: "remote-worker", Tool: "claude", Group: "work", Directory: "/home/me", Status: "idle", Running: true}, Host: "gpu"},
	}
	if !reflect.DeepEqual(sessions.Sessions, want) {
		t.Fatalf("sessions = %+v\nwant %+v", sessions.Sessions, want)
	}
	if len(sessions.ConnectionErrors) != 1 || sessions.ConnectionErrors[0].Host != "cpu" || !strings.Contains(sessions.ConnectionErrors[0].Error, "Connection refused") {
		t.Fatalf("connection errors = %+v", sessions.ConnectionErrors)
	}
	text := resultText(result)
	for _, want := range []string{"lead (id cafe)", "SSH connection gpu:\n- remote-worker (id gpu::r1)", "SSH connection cpu (not refreshed: unreachable over SSH"} {
		if !strings.Contains(text, want) {
			t.Fatalf("list text lacks %q:\n%s", want, text)
		}
	}

	var terminals listTerminalsOutput
	structured(t, callTool(t, f.session, "list_terminals", map[string]any{}), &terminals)
	if len(terminals.Terminals) != 2 {
		t.Fatalf("terminals = %+v, want the local one and gpu's unarchived one", terminals.Terminals)
	}
	if got := terminals.Terminals[1]; got.ID != "gpu::t1" || got.ParentID != "gpu::r1" || got.Host != "gpu" || got.Stale {
		t.Fatalf("remote terminal = %+v", got)
	}

	var groups listGroupsOutput
	structured(t, callTool(t, f.session, "list_groups", map[string]any{}), &groups)
	if len(groups.Groups) != 1 || groups.Groups[0].Path != "work" || groups.Groups[0].Host != "gpu" {
		t.Fatalf("groups = %+v", groups.Groups)
	}
}

func TestAnUnreachableHostKeepsItsLastRowsMarkedStale(t *testing.T) {
	var down sync.Mutex
	offline := false
	f := newRemoteFixture(t, "cafe", func(string, []string) ([]byte, []byte, error) {
		down.Lock()
		defer down.Unlock()
		if offline {
			return nil, []byte("ssh: connect to host gpu port 22: Operation timed out"), exitStatus(255)
		}
		return mustJSON(t, gpuSnapshot()), nil, nil
	})
	callTool(t, f.session, "list_sessions", map[string]any{})
	down.Lock()
	offline = true
	down.Unlock()

	result := callTool(t, f.session, "list_sessions", map[string]any{})
	var sessions listSessionsOutput
	structured(t, result, &sessions)
	if len(sessions.Sessions) != 2 || sessions.Sessions[1].ID != "gpu::r1" || !sessions.Sessions[1].Stale {
		t.Fatalf("sessions = %+v, want gpu's last row marked stale", sessions.Sessions)
	}
	if sessions.Sessions[0].Stale {
		t.Fatal("local row marked stale")
	}
	if len(sessions.ConnectionErrors) != 1 || !strings.Contains(sessions.ConnectionErrors[0].Error, "Operation timed out") {
		t.Fatalf("connection errors = %+v", sessions.ConnectionErrors)
	}
	if text := resultText(result); !strings.Contains(text, "these are the rows it last reported") {
		t.Fatalf("text does not say the rows are old:\n%s", text)
	}
	f.hosts.reset()
	result = callTool(t, f.session, "list_sessions", map[string]any{})
	if calls := f.hosts.recorded(); len(calls) != 0 {
		t.Fatalf("a host inside its retry wait was dialled again: %+v", calls)
	}
	structured(t, result, &sessions)
	if len(sessions.Sessions) != 2 || !sessions.Sessions[1].Stale || len(sessions.ConnectionErrors) != 1 {
		t.Fatalf("resting host listing = %+v", sessions)
	}
}

func TestListingsPickUpAConnectionAddedWhileServing(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(string, []string) ([]byte, []byte, error) {
		return mustJSON(t, gpuSnapshot()), nil, nil
	})
	f.connections = nil
	if text, _ := callText(t, f.session, "list_sessions", map[string]any{}); strings.Contains(text, "gpu") || len(f.hosts.recorded()) != 0 {
		t.Fatalf("listed with no connections: %q, calls %v", text, f.hosts.recorded())
	}
	f.connections = []store.Connection{{Name: "gpu", Destination: "me@gpu"}}
	if text, _ := callText(t, f.session, "list_sessions", map[string]any{}); !strings.Contains(text, "gpu::r1") {
		t.Fatalf("new connection missing from the list:\n%s", text)
	}
	f.connections = nil
	if text, _ := callText(t, f.session, "list_sessions", map[string]any{}); strings.Contains(text, "gpu") {
		t.Fatalf("removed connection still listed:\n%s", text)
	}
}

func TestListingsWithNoConnectionsKeepTheirOutput(t *testing.T) {
	f := newRemoteFixture(t, "cafe", nil)
	f.connections = nil
	f.local.groups = []sessioncmd.Group{{Path: "work", Sessions: 1}}
	for _, tc := range []struct {
		tool string
		text string
		want any
	}{
		{"list_sessions", sessioncmd.FormatSessionList(f.local.listed), struct {
			Sessions []sessioncmd.Session `json:"sessions"`
		}{f.local.listed}},
		{"list_terminals", sessioncmd.FormatTerminalList(f.terminals.listed), struct {
			Terminals []sessioncmd.Terminal `json:"terminals"`
		}{f.terminals.listed}},
		{"list_groups", sessioncmd.FormatGroupList(f.local.groups), struct {
			Groups []sessioncmd.Group `json:"groups"`
		}{f.local.groups}},
	} {
		result := callTool(t, f.session, tc.tool, map[string]any{})
		if text := resultText(result); text != tc.text {
			t.Errorf("%s text = %q, want %q", tc.tool, text, tc.text)
		}
		var want any
		if err := json.Unmarshal(mustJSON(t, tc.want), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.StructuredContent, want) {
			t.Errorf("%s structured = %v, want %v", tc.tool, result.StructuredContent, want)
		}
	}
	if calls := f.hosts.recorded(); len(calls) != 0 {
		t.Fatalf("ssh ran with no connections: %v", calls)
	}
	// The server marshals the row types, so a local row keeps its bytes.
	for _, pair := range [][2]any{
		{sessionRow{Session: f.local.listed[0]}, f.local.listed[0]},
		{terminalRow{Terminal: f.terminals.listed[0]}, f.terminals.listed[0]},
		{groupRow{Group: f.local.groups[0]}, f.local.groups[0]},
	} {
		if got, want := mustJSON(t, pair[0]), mustJSON(t, pair[1]); string(got) != string(want) {
			t.Errorf("local row = %s, want %s", got, want)
		}
	}
}

func TestQualifiedIDsRouteToTheirHost(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	hostname, _, _ = strings.Cut(hostname, ".")
	remoteSession := sessioncmd.Session{ID: "r1", Name: "remote-worker", Tool: "claude", Status: "idle"}
	remoteTerminal := sessioncmd.Terminal{ID: "t1", Name: "shell"}
	f := newRemoteFixture(t, "cafe", func(destination string, words []string) ([]byte, []byte, error) {
		if destination != "me@gpu" {
			t.Errorf("call went to %s", destination)
		}
		switch strings.Join(words[:min(2, len(words))], " ") {
		case "read --json":
			return mustJSON(t, sessioncmd.SessionScreen{Session: remoteSession, Output: "remote screen"}), nil, nil
		case "send --from=lead on " + hostname:
			return mustJSON(t, sessioncmd.SendResult{MessageID: 3, QueuePosition: 1, ManagerAwake: true}), nil, nil
		case "terminal read":
			return mustJSON(t, sessioncmd.TerminalScreen{Terminal: remoteTerminal, Output: "$ ls"}), nil, nil
		case "terminal send":
			return mustJSON(t, sessioncmd.TerminalInput{TerminalID: "t1", Sent: "keys"}), nil, nil
		case "terminal close":
			return []byte("closed terminal t1\n"), nil, nil
		case "terminal create":
			return mustJSON(t, remoteTerminal), nil, nil
		case "create-group --json":
			return mustJSON(t, sessioncmd.Group{Path: "work"}), nil, nil
		case "delete-group --json":
			return mustJSON(t, sessioncmd.GroupRemoval{Removed: []string{"work"}, Moved: []string{"r1"}}), nil, nil
		}
		return mustJSON(t, remoteSession), nil, nil
	})

	for _, tc := range []struct {
		tool  string
		args  map[string]any
		words []string
		text  string
	}{
		{"read_session", map[string]any{"session_id": "gpu::r1"}, []string{"read", "--json", "--", "r1"}, "remote screen"},
		{"send_session", map[string]any{"session_id": "gpu::r1", "message": "rebase on main"},
			[]string{"send", "--from=lead on " + hostname, "--json", "--", "r1", "rebase on main"}, "queued a message for session gpu::r1"},
		{"kill_session", map[string]any{"session_id": "gpu::r1"}, []string{"kill", "--json", "--", "r1"}, "killed remote-worker (id gpu::r1)"},
		{"revive_session", map[string]any{"session_id": "gpu::r1"}, []string{"revive", "--json", "--", "r1"}, "revived remote-worker (id gpu::r1)"},
		{"archive_session", map[string]any{"session_id": "gpu::r1"}, []string{"archive", "--json", "--", "r1"}, "(id gpu::r1)"},
		{"archive_session", map[string]any{"session_id": "gpu::r1", "archived": false}, []string{"archive", "--restore", "--json", "--", "r1"}, "restored remote-worker (id gpu::r1)"},
		// The terminal tools below drive the one this session opens here.
		{"create_terminal", map[string]any{"host": "gpu", "directory": "/srv"}, []string{"terminal", "create", "--json", "--directory=/srv"}, "created shell (id gpu::t1)"},
		{"read_terminal", map[string]any{"terminal_id": "gpu::t1"}, []string{"terminal", "read", "--json", "--", "t1"}, "$ ls"},
		{"send_terminal", map[string]any{"terminal_id": "gpu::t1", "keys": []string{"C-c", "Up"}},
			[]string{"terminal", "send", "--json", "--keys=C-c", "--keys=Up", "--", "t1"}, "sent keys to terminal gpu::t1"},
		{"close_terminal", map[string]any{"terminal_id": "gpu::t1"}, []string{"terminal", "close", "--", "t1"}, "closed terminal gpu::t1"},
		{"create_session", map[string]any{"host": "gpu", "name": "remote-fix", "prompt": "fix it", "tool": "codex", "group": "work", "worktree": true},
			[]string{"spawn", "--json", "--name=remote-fix", "--prompt=fix it", "--tool=codex", "--group=work", "--worktree=true"}, "created remote-worker (id gpu::r1)"},
		// Saved prompts are this machine's, so the host gets their text.
		{"create_session", map[string]any{"host": "gpu", "prompt": "fix it", "saved": "reviewer"},
			[]string{"spawn", "--json", "--prompt=saved:reviewer\n\nfix it"}, "created remote-worker (id gpu::r1)"},
		{"send_session", map[string]any{"session_id": "gpu::r1", "message": "rebase on main", "saved": "reviewer"},
			[]string{"send", "--from=lead on " + hostname, "--json", "--", "r1", "saved:reviewer\n\nrebase on main"}, "queued a message for session gpu::r1"},
		{"create_group", map[string]any{"host": "gpu", "path": "work", "directory": "/srv"}, []string{"create-group", "--json", "--directory=/srv", "--", "work"}, "created group work on gpu"},
		{"delete_group", map[string]any{"host": "gpu", "path": "work"}, []string{"delete-group", "--json", "--", "work"}, "deleted work; 1 session(s) moved to the root group: gpu::r1 on gpu"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			f.hosts.reset()
			text, isError := callText(t, f.session, tc.tool, tc.args)
			if isError || !strings.Contains(text, tc.text) {
				t.Fatalf("%s = %q (error %v), want %q", tc.tool, text, isError, tc.text)
			}
			calls := f.hosts.recorded()
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].words, tc.words) {
				t.Fatalf("remote calls = %+v, want one running %q", calls, tc.words)
			}
		})
	}
	if f.local.readID != "" || f.local.sentID != "" || f.local.killedID != "" || f.local.revivedID != "" || f.local.archivedID != "" ||
		f.local.groupPath != "" || f.local.createdOpts.Name != "" ||
		f.terminals.readID != "" || f.terminals.sentID != "" || f.terminals.closedID != "" || f.terminals.createdOpts.Directory != "" {
		t.Fatalf("a routed call also ran locally: sessions %+v terminals %+v", f.local, f.terminals)
	}
}

// A read answers with the row the host reports, not one cached by an
// earlier listing in this process, so a server that has listed nothing yet
// still reports whether the session runs.
func TestRemoteReadsCarryTheHostsRow(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(_ string, words []string) ([]byte, []byte, error) {
		if words[0] == "terminal" {
			return mustJSON(t, sessioncmd.TerminalScreen{Terminal: sessioncmd.Terminal{ID: "t1", Name: "shell", ParentID: "r1", Running: true}, Output: "$ ls"}), nil, nil
		}
		return mustJSON(t, sessioncmd.SessionScreen{Session: sessioncmd.Session{ID: "r1", Name: "remote-worker", Status: "working", Running: true}, Output: "screen"}), nil, nil
	})
	var session sessioncmd.SessionScreen
	structured(t, callTool(t, f.session, "read_session", map[string]any{"session_id": "gpu::r1"}), &session)
	if want := (sessioncmd.Session{ID: "gpu::r1", Name: "remote-worker", Status: "working", Running: true}); session.Session != want {
		t.Fatalf("read_session row = %+v, want %+v", session.Session, want)
	}
	var terminal sessioncmd.TerminalScreen
	structured(t, callTool(t, f.session, "read_terminal", map[string]any{"terminal_id": "gpu::t1"}), &terminal)
	if want := (sessioncmd.Terminal{ID: "gpu::t1", Name: "shell", ParentID: "gpu::r1", Running: true}); terminal.Terminal != want {
		t.Fatalf("read_terminal row = %+v, want %+v", terminal.Terminal, want)
	}
}

func TestARemoteRefusalReachesTheAgent(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(string, []string) ([]byte, []byte, error) {
		return nil, []byte("agent-manager: terminal t1 is nested under remote-worker; only that session drives it\n"), exitStatus(1)
	})
	if err := f.withStore(func(st *store.Store) error { return st.SetRemoteTerminalOpener("gpu", "t1", "cafe") }); err != nil {
		t.Fatal(err)
	}
	text, isError := callText(t, f.session, "send_terminal", map[string]any{"terminal_id": "gpu::t1", "command": "ls"})
	if !isError || text != "gpu: terminal t1 is nested under remote-worker; only that session drives it" {
		t.Fatalf("send_terminal = %q, error %v", text, isError)
	}
}

// The host sees every SSH call as one caller with no session, so this side
// keeps one agent's terminal there out of another agent's reach, and the
// user's own terminal there, opened with T on the connection row, out of
// every agent's.
func TestARemoteTerminalIsDrivenOnlyByTheSessionThatOpenedIt(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(_ string, words []string) ([]byte, []byte, error) {
		switch strings.Join(words[:2], " ") {
		case "terminal create":
			return mustJSON(t, sessioncmd.Terminal{ID: "t9", Name: "shell"}), nil, nil
		case "terminal send":
			return mustJSON(t, sessioncmd.TerminalInput{TerminalID: "t9", Sent: "command"}), nil, nil
		case "terminal read":
			return mustJSON(t, sessioncmd.TerminalScreen{Terminal: sessioncmd.Terminal{ID: words[len(words)-1], Name: "shell"}, Output: "$"}), nil, nil
		case "terminal close":
			return []byte("closed terminal t9\n"), nil, nil
		}
		t.Errorf("unexpected remote call %q", words)
		return nil, nil, nil
	})
	other := f.connect(t, "dead")
	if text, isError := callText(t, f.session, "create_terminal", map[string]any{"host": "gpu"}); isError {
		t.Fatalf("create_terminal = %q", text)
	}
	if got := f.opener(t, "gpu", "t9"); got != "cafe" {
		t.Fatalf("opener of gpu::t9 = %q, want cafe", got)
	}

	f.hosts.reset()
	for _, tc := range []struct {
		session *mcp.ClientSession
		id      string
	}{
		{other, "gpu::t9"},
		{other, "gpu::t5"},
		{f.session, "gpu::t5"},
	} {
		want := "terminal " + tc.id + " was not opened by this session; a session drives only the terminals it opened on gpu with create_terminal"
		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"send_terminal", map[string]any{"terminal_id": tc.id, "command": "ls"}},
			{"close_terminal", map[string]any{"terminal_id": tc.id}},
		} {
			if text, isError := callText(t, tc.session, call.tool, call.args); !isError || text != want {
				t.Errorf("%s on %s = %q, error %v; want %q", call.tool, tc.id, text, isError, want)
			}
		}
	}
	if calls := f.hosts.recorded(); len(calls) != 0 {
		t.Fatalf("a refused call reached ssh: %+v", calls)
	}

	// Reading stays open to every session, as it is for this machine's terminals.
	if text, isError := callText(t, other, "read_terminal", map[string]any{"terminal_id": "gpu::t9"}); isError {
		t.Fatalf("read_terminal = %q", text)
	}
	if text, isError := callText(t, f.session, "send_terminal", map[string]any{"terminal_id": "gpu::t9", "command": "ls"}); isError {
		t.Fatalf("send_terminal from its opener = %q", text)
	}
	if text, isError := callText(t, f.session, "close_terminal", map[string]any{"terminal_id": "gpu::t9"}); isError {
		t.Fatalf("close_terminal from its opener = %q", text)
	}
	if got := f.opener(t, "gpu", "t9"); got != "" {
		t.Fatalf("closing kept the opener %q", got)
	}
}

func TestLocalOnlyToolsRefuseQualifiedIDsWithoutIO(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(string, []string) ([]byte, []byte, error) {
		t.Error("a refused call reached ssh")
		return nil, nil, nil
	})
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"wait_for_session", map[string]any{"session_id": "gpu::r1"}},
		{"task", map[string]any{"action": "claim", "task_id": "gpu::7"}},
		{"task", map[string]any{"action": "create", "title": "t", "body": "b", "depends_on": []string{"gpu::7"}}},
		{"review_comment", map[string]any{"comment_id": "gpu::0123456789abcdef"}},
	} {
		text, isError := callText(t, f.session, tc.tool, tc.args)
		if !isError || text != tc.tool+" works on this host's sessions only" {
			t.Errorf("%s = %q, error %v", tc.tool, text, isError)
		}
	}
	if f.local.waitedID != "" || f.local.claimedTaskID != "" || f.local.taskTitle != "" || f.reads != 0 {
		t.Fatalf("a refused call did work: sessions %+v, connection reads %d", f.local, f.reads)
	}
}

func TestRemoteToolsStayClosedWithoutACaller(t *testing.T) {
	f := newRemoteFixture(t, "", func(string, []string) ([]byte, []byte, error) {
		t.Error("a caller outside Agent Manager reached ssh")
		return nil, nil, nil
	})
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"send_session", map[string]any{"session_id": "gpu::r1", "message": "hi"}},
		{"kill_session", map[string]any{"session_id": "gpu::r1"}},
		{"send_terminal", map[string]any{"terminal_id": "gpu::t1", "command": "ls"}},
		{"create_group", map[string]any{"host": "gpu", "path": "work"}},
	} {
		if _, isError := callText(t, f.session, tc.tool, tc.args); !isError {
			t.Errorf("%s ran without a caller", tc.tool)
		}
	}
	if f.reads != 0 {
		t.Fatalf("read connections %d times without a caller", f.reads)
	}
}

func TestSenderNameFitsTheRemoteLimit(t *testing.T) {
	for _, tc := range []struct {
		name, host, want string
	}{
		{"lead", "laptop", "lead on laptop"},
		{strings.Repeat("a", 70), "laptop", strings.Repeat("a", maxSenderBytes-len(" on laptop")) + " on laptop"},
		{strings.Repeat("é", 40), "laptop", strings.Repeat("é", 27) + " on laptop"},
		{"lead", strings.Repeat("h", 70), ("lead on " + strings.Repeat("h", 70))[:maxSenderBytes]},
	} {
		got := senderName(tc.name, tc.host)
		if got != tc.want || len(got) > maxSenderBytes {
			t.Errorf("senderName(%q, %q) = %q, want %q", tc.name, tc.host, got, tc.want)
		}
	}
}

// A terminal no session could drive is closed again rather than left
// running on the host.
func TestARemoteTerminalWhoseOpenerIsNotRecordedIsClosed(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(_ string, words []string) ([]byte, []byte, error) {
		switch strings.Join(words[:2], " ") {
		case "terminal create":
			return mustJSON(t, sessioncmd.Terminal{ID: "t9", Name: "shell"}), nil, nil
		case "terminal close":
			return []byte("closed terminal t9\n"), nil, nil
		}
		t.Errorf("unexpected remote call %q", words)
		return nil, nil, nil
	})
	f.withStore = func(func(*store.Store) error) error { return errors.New("disk I/O error") }
	session := f.connect(t, "cafe")
	f.hosts.reset()
	text, isError := callText(t, session, "create_terminal", map[string]any{"host": "gpu"})
	if !isError || !strings.Contains(text, "so it was closed again: disk I/O error") {
		t.Fatalf("create_terminal = %q", text)
	}
	var words []string
	for _, call := range f.hosts.recorded() {
		words = append(words, strings.Join(call.words[:2], " "))
	}
	if !slices.Equal(words, []string{"terminal create", "terminal close"}) {
		t.Fatalf("calls = %q, want the terminal created and closed", words)
	}
}
