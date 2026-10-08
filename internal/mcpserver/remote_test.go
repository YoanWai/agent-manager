package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
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
	call := hostCall{destination: argv[len(argv)-2], words: remoteArgs(f.t, argv[len(argv)-1])}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	answer := f.answer
	f.mu.Unlock()
	return answer(call.destination, call.words)
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

// remoteArgs undoes the login-shell quoting of the remote command and drops
// the agent-manager word.
func remoteArgs(t *testing.T, command string) []string {
	t.Helper()
	inner, ok := strings.CutPrefix(command, `exec "$SHELL" -lc `)
	if !ok {
		t.Fatalf("remote command %q does not go through the login shell", command)
	}
	outer := splitQuoted(t, inner)
	if len(outer) != 1 {
		t.Fatalf("login shell gets %d words, want 1", len(outer))
	}
	words := splitQuoted(t, outer[0])
	if len(words) == 0 || words[0] != "agent-manager" {
		t.Fatalf("remote command runs %v", words)
	}
	return words[1:]
}

func splitQuoted(t *testing.T, line string) []string {
	t.Helper()
	var words []string
	var word strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; ch {
		case ' ':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		case '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				t.Fatalf("unterminated quote in %q", line)
			}
			word.WriteString(line[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case '\\':
			i++
			word.WriteByte(line[i])
			inWord = true
		default:
			t.Fatalf("unquoted %q in %q", ch, line)
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
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
	profile := t.TempDir()
	// The client keeps its control sockets under /tmp, named after the
	// profile the way internal/remote names them.
	sum := sha256.Sum256([]byte(profile))
	t.Cleanup(func() { os.RemoveAll(filepath.Join("/tmp", "am-ssh-"+hex.EncodeToString(sum[:])[:8])) })
	source := remoteSource{
		client: remote.New(profile, remote.WithRunner(f.hosts.run)),
		connections: func() ([]store.Connection, error) {
			f.reads++
			return slices.Clone(f.connections), nil
		},
	}
	f.session = connectServer(t, newServerWithMailbox(sessionID, "test", true, f.terminals, f.local, &fakeReporter{}, sessioncmd.NewMailbox(profile), source))
	return f
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
			[]string{"send", "--from=lead on " + hostname, "--json", "--", "r1", "rebase on main"}, "queued message 3 for session gpu::r1"},
		{"kill_session", map[string]any{"session_id": "gpu::r1"}, []string{"kill", "--json", "--", "r1"}, "killed remote-worker (id gpu::r1)"},
		{"revive_session", map[string]any{"session_id": "gpu::r1"}, []string{"revive", "--json", "--", "r1"}, "revived remote-worker (id gpu::r1)"},
		{"archive_session", map[string]any{"session_id": "gpu::r1"}, []string{"archive", "--json", "--", "r1"}, "(id gpu::r1)"},
		{"archive_session", map[string]any{"session_id": "gpu::r1", "archived": false}, []string{"archive", "--restore", "--json", "--", "r1"}, "restored remote-worker (id gpu::r1)"},
		{"read_terminal", map[string]any{"terminal_id": "gpu::t1"}, []string{"terminal", "read", "--json", "--", "t1"}, "$ ls"},
		{"send_terminal", map[string]any{"terminal_id": "gpu::t1", "keys": []string{"C-c", "Up"}},
			[]string{"terminal", "send", "--json", "--keys=C-c", "--keys=Up", "--", "t1"}, "sent keys to terminal gpu::t1"},
		{"close_terminal", map[string]any{"terminal_id": "gpu::t1"}, []string{"terminal", "close", "--", "t1"}, "closed terminal gpu::t1"},
		{"create_session", map[string]any{"host": "gpu", "name": "remote-fix", "prompt": "fix it", "tool": "codex", "group": "work", "worktree": true},
			[]string{"spawn", "--json", "--name=remote-fix", "--prompt=fix it", "--tool=codex", "--group=work", "--worktree=true"}, "created remote-worker (id gpu::r1)"},
		{"create_terminal", map[string]any{"host": "gpu", "directory": "/srv"}, []string{"terminal", "create", "--json", "--directory=/srv"}, "created shell (id gpu::t1)"},
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

func TestARemoteRefusalReachesTheAgent(t *testing.T) {
	f := newRemoteFixture(t, "cafe", func(string, []string) ([]byte, []byte, error) {
		return nil, []byte("agent-manager: terminal t1 is nested under remote-worker; only that session drives it\n"), exitStatus(1)
	})
	text, isError := callText(t, f.session, "send_terminal", map[string]any{"terminal_id": "gpu::t1", "command": "ls"})
	if !isError || text != "gpu: terminal t1 is nested under remote-worker; only that session drives it" {
		t.Fatalf("send_terminal = %q, error %v", text, isError)
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
