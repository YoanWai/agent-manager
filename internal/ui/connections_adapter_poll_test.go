package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/x/ansi"
)

// fakeSSH stands in for ssh: it records the agent-manager words each call
// carried and answers as the remote CLI would.
type fakeSSH struct {
	mu       sync.Mutex
	calls    [][]string
	snapshot sessioncmd.Snapshot
	screen   string
	// stderr and code fail every call the way a remote command does.
	stderr string
	code   int
}

type fakeExit int

func (e fakeExit) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e fakeExit) ExitCode() int { return int(e) }

func (f *fakeSSH) run(ctx context.Context, argv []string) ([]byte, []byte, error) {
	stdout, stderr, err := f.answer(ctx, argv)
	return remotetest.Answer(stdout), stderr, err
}

func (f *fakeSSH) answer(_ context.Context, argv []string) ([]byte, []byte, error) {
	all, _ := remotetest.Words(argv)
	words := all[1:]
	f.mu.Lock()
	f.calls = append(f.calls, words)
	snapshot, screen, stderr, code := f.snapshot, f.screen, f.stderr, f.code
	f.mu.Unlock()
	if code != 0 {
		return nil, []byte(stderr), fakeExit(code)
	}
	var reply any
	switch words[0] {
	case "snapshot":
		reply = snapshot
	case "serve":
		reply = map[string]bool{"started": false}
	case "send":
		reply = sessioncmd.SendResult{MessageID: 7, QueuePosition: 1, ManagerAwake: true}
	case "read":
		reply = sessioncmd.SessionScreen{Session: sessioncmd.Session{ID: words[len(words)-1]}, Output: screen}
	case "terminal":
		switch words[1] {
		case "close":
			return []byte("closed\n"), nil, nil
		case "read":
			reply = sessioncmd.TerminalScreen{Terminal: sessioncmd.Terminal{ID: words[len(words)-1]}, Output: screen}
		case "send":
			reply = sessioncmd.TerminalInput{TerminalID: words[len(words)-1], Sent: "command"}
		default:
			reply = sessioncmd.Terminal{ID: "t9", Name: "shell"}
		}
	default:
		reply = sessioncmd.Session{ID: "s9", Name: "fresh"}
	}
	out, err := json.Marshal(reply)
	return out, nil, err
}

func (f *fakeSSH) taken() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func boxSnapshot() sessioncmd.Snapshot {
	return sessioncmd.Snapshot{
		Version:      sessioncmd.SnapshotVersion,
		ManagerAwake: true,
		Groups:       []sessioncmd.Group{{Path: "web"}},
		Sessions: []sessioncmd.Session{
			{ID: "s1", Name: "api", Tool: "claude", Group: "web", Status: "idle", Running: true},
			{ID: "s2", Name: "old", Tool: "claude", Group: "web", Status: "dead"},
			{ID: "s3", Name: "shelved", Tool: "claude", Group: "web", Status: "dead", Archived: true},
		},
		Terminals: []sessioncmd.Terminal{{ID: "t1", Name: "sh", Group: "web", Status: "idle", Running: true}},
	}
}

// listedModel lists one connection, box, not refreshed yet, with every
// call going to fake.
func listedModel(t *testing.T, fake *fakeSSH) *Model {
	t.Helper()
	m := buildModel(t)
	fake.snapshot = boxSnapshot()
	m.ssh = uilink.New(testRemoteClient(t, remote.WithRunner(fake.run)), []store.Connection{{Name: "box", Destination: "me@box"}})
	m.rebuildRows()
	return m
}

// connectedModel is listedModel with box as its last refresh left it.
func connectedModel(t *testing.T, fake *fakeSSH) *Model {
	t.Helper()
	m := listedModel(t, fake)
	setHost(m, "box", uilink.Facts(remote.HostState{OK: true, Snapshot: fake.snapshot}))
	return m
}

// setHost lands state as host's latest refresh.
func setHost(m *Model, host string, state uiconnections.Host) {
	m.ssh.Refreshed(host, m.ssh.Gen(), state)
	m.rebuildRows()
}

func hostState(m *Model, host string) uiconnections.Host {
	state, _ := m.ssh.Host(host)
	return state
}

// testRemoteClient makes a client on a throwaway profile and removes the
// control directory the first call creates under /tmp.
func testRemoteClient(t *testing.T, opts ...remote.Option) *remote.Client {
	t.Helper()
	profile := t.TempDir()
	t.Cleanup(func() { os.RemoveAll(remote.ControlDir(profile)) })
	return remote.New(profile, opts...)
}

var (
	boxRow   = uirail.Selection{Kind: uirail.ConnectionRow, Host: "box"}
	boxGroup = uirail.Selection{Kind: uirail.GroupRow, Group: "web", Host: "box"}
)

func boxSession(id string) uirail.Selection {
	return uirail.Selection{Kind: uirail.SessionRow, SessionID: id, Group: "web", Host: "box"}
}

func railNames(m *Model) []string {
	var names []string
	for _, row := range m.rail.Rows() {
		names = append(names, row.Name)
	}
	return names
}

func TestPollRefreshesEachHostOnceAtATimeOffTheUpdatePath(t *testing.T) {
	fake := &fakeSSH{}
	m := listedModel(t, fake)

	cmd := m.ssh.PollHost("box")
	if cmd == nil {
		t.Fatal("no refresh started")
	}
	if len(fake.taken()) != 0 {
		t.Fatal("the refresh ran on the update path")
	}
	if again := m.ssh.PollHost("box"); again != nil {
		t.Fatal("a second refresh started while the first was in flight")
	}
	m.applyTestMsg(t, cmd())
	if calls := fake.taken(); len(calls) != 1 || calls[0][0] != "snapshot" {
		t.Fatalf("calls = %q, want one snapshot", calls)
	}
	if state := hostState(m, "box"); !state.OK {
		t.Fatalf("state %+v after the refresh landed", state)
	}
	if names := railNames(m); !slices.Contains(names, "api") || !slices.Contains(names, "box") {
		t.Fatalf("rows = %q, want box and its agents", names)
	}
}

func TestOfflineHostKeepsItsLastRows(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.applyTestMsg(t, m.ssh.PollHost("box")())
	fake.mu.Lock()
	fake.stderr, fake.code = "ssh: connect to host box port 22: Connection refused", 255
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())

	state := hostState(m, "box")
	if state.OK || state.Failures != 1 || !strings.Contains(state.Err, "Connection refused") {
		t.Fatalf("state = %+v", state)
	}
	if names := railNames(m); !slices.Contains(names, "api") || !slices.Contains(names, "sh") {
		t.Fatalf("rows = %q, want the last snapshot kept", names)
	}
	if host := m.ssh.RailHosts()[0]; host.State != uirail.HostOffline || host.Trouble != "unreachable" {
		t.Fatalf("rail host = %+v, want offline and unreachable", host)
	}
}

// A host ssh reaches says what is wrong with agent-manager there rather
// than reading as down.
func TestFailedHostNamesItsTrouble(t *testing.T) {
	for _, tc := range []struct {
		version int
		stderr  string
		code    int
		want    string
	}{
		{sessioncmd.SnapshotVersion, "sh: agent-manager: not found", 127, "agent-manager missing"},
		{sessioncmd.SnapshotVersion - 1, "", 0, "agent-manager too old"},
		{sessioncmd.SnapshotVersion + 1, "", 0, "agent-manager newer"},
		{sessioncmd.SnapshotVersion, "agent-manager: database is locked", 1, "error"},
	} {
		fake := &fakeSSH{}
		m := connectedModel(t, fake)
		fake.mu.Lock()
		fake.snapshot.Version, fake.stderr, fake.code = tc.version, tc.stderr, tc.code
		fake.mu.Unlock()
		m.applyTestMsg(t, m.ssh.PollHost("box")())
		if host := m.ssh.RailHosts()[0]; host.Trouble != tc.want {
			t.Errorf("version %d, %q: trouble %q, want %q", tc.version, tc.stderr, host.Trouble, tc.want)
		}
	}
}

// A host that failed its last refresh is not dialled again on the next
// poll; it waits out remote.RetryWait for its failure count first.
func TestUnreachableHostRestsBetweenRetries(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	fake.mu.Lock()
	fake.stderr, fake.code = "ssh: connect to host box port 22: Connection refused", 255
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())
	if m.ssh.PollHost("box") != nil {
		t.Fatal("a host that just failed was dialled again at once")
	}
	state := hostState(m, "box")
	state.Attempted = time.Now().Add(-remote.RetryWait(state.Failures))
	setHost(m, "box", state)
	cmd := m.ssh.PollHost("box")
	if cmd == nil {
		t.Fatal("a host past its wait was not dialled")
	}
	m.applyTestMsg(t, cmd())
	if got := hostState(m, "box").Failures; got != 2 {
		t.Fatalf("failures = %d, want 2", got)
	}
}

func TestRefreshOfAReplacedListIsDropped(t *testing.T) {
	fake := &fakeSSH{}
	m := listedModel(t, fake)
	stale := m.ssh.PollHost("box")
	m.ssh.SetList([]store.Connection{{Name: "box", Destination: "me@other"}})
	m.applyTestMsg(t, stale())
	if _, seen := m.ssh.Host("box"); seen {
		t.Fatal("a refresh of the old destination landed on the new one")
	}
}

func TestRemovedConnectionTakesItsRowsWithIt(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.ssh.SetList(nil)
	m.rebuildRows()
	if names := railNames(m); slices.Contains(names, "box") || slices.Contains(names, "api") {
		t.Fatalf("rows = %q after removal", names)
	}
}

func contentText(m *Model) string {
	var lines []string
	for _, line := range m.contentLines(80, 20) {
		lines = append(lines, ansi.Strip(line.text))
	}
	return strings.Join(lines, "\n")
}

func TestRemotePreviewReadsTheSelectedScreenOffTheUpdatePath(t *testing.T) {
	fake := &fakeSSH{screen: "\x1b[1mbuilding\x1b[0m api\n$ "}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	cmd := m.syncRemotePreview()
	if cmd == nil || len(fake.taken()) != 0 {
		t.Fatal("the read was not deferred to a command")
	}
	if text := contentText(m); !strings.Contains(text, "api  on box") || !strings.Contains(text, "reading the screen over SSH") {
		t.Fatalf("content before the read:\n%s", text)
	}
	m.applyTestMsg(t, cmd())
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], []string{"read", "--styled", "--json", "--", "s1"}) {
		t.Fatalf("calls = %q", calls)
	}
	if text := contentText(m); !strings.Contains(text, "building api") || strings.Contains(text, "\x1b") {
		t.Fatalf("content after the read:\n%s", text)
	}
}

func TestRemotePreviewReadsATerminalAsOne(t *testing.T) {
	fake := &fakeSSH{screen: "$ ls"}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("t1"))
	m.applyTestMsg(t, m.syncRemotePreview()())
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], []string{"terminal", "read", "--styled", "--json", "--", "t1"}) {
		t.Fatalf("calls = %q", calls)
	}
}

func TestRemotePreviewDropsAReadForAnEarlierSelection(t *testing.T) {
	fake := &fakeSSH{screen: "old screen"}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	stale := m.syncRemotePreview()
	m.rail.Focus(boxSession("s2"))
	m.syncRemotePreview()
	m.applyTestMsg(t, stale())
	if text := contentText(m); strings.Contains(text, "old screen") || !strings.Contains(text, "reading the screen over SSH") {
		t.Fatalf("content = %q, want the s1 read dropped while s2 reads", text)
	}
}

func TestConnectionRowShowsItsFacts(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxRow)
	text := contentText(m)
	for _, want := range []string{"box", "online", "me@box", "3 sessions · 1 terminal · manager awake"} {
		if !strings.Contains(text, want) {
			t.Fatalf("content lacks %q:\n%s", want, text)
		}
	}
}

// hostile is what a compromised host could put in any string it returns:
// an OSC 52 clipboard write, a carriage return, a backspace and a bell.
const hostile = "\x1b]52;c;cHduZWQ=\x07\r\b\x07"

// A connection's host is not trusted to drive the terminal: whatever it
// names, reports or prints reaches a frame as text.
func TestHostTextCannotDriveTheTerminal(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.layout.width, m.layout.height = 120, 40
	group := "web" + hostile
	fake.mu.Lock()
	fake.snapshot.Groups = []sessioncmd.Group{{Path: group}}
	fake.snapshot.Sessions = []sessioncmd.Session{{ID: "s1", Name: "api" + hostile, Tool: "claude" + hostile, Group: group, Status: "idle" + hostile}}
	fake.snapshot.Terminals = nil
	fake.screen = "build" + hostile + "ing\x1bc\x0e\x0fdone\n$ "
	raw := fake.snapshot
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())
	// internal/remote already cleans what a host answers; the UI escapes on
	// its own too, so this test hands it the host's raw rows.
	setHost(m, "box", uilink.Facts(remote.HostState{OK: true, Snapshot: raw}))

	frame := func(surface string) {
		t.Helper()
		view := preparedView(m)
		if stray := strayControl(view); stray != "" {
			t.Fatalf("%s leaks a control byte near %q", surface, stray)
		}
		if !strings.Contains(view, "52;c;cHduZWQ=") {
			t.Fatalf("%s: the host's text should be on screen to be a real test:\n%s", surface, ansi.Strip(view))
		}
	}
	session := uirail.Selection{Kind: uirail.SessionRow, SessionID: "s1", Group: group, Host: "box"}
	m.rail.Focus(session)
	m.applyTestMsg(t, m.syncRemotePreview()())
	if text := contentText(m); !strings.Contains(text, "building") || !strings.Contains(text, "done") {
		t.Fatalf("the screen lost its text:\n%s", text)
	}
	frame("the rail and the remote screen")

	m.rail.Focus(uirail.Selection{Kind: uirail.GroupRow, Group: group, Host: "box"})
	frame("a remote group")
	m.openForm()
	m.form().FocusField(m.spawnHost(), spawn.FieldGroup)
	frame("the New Session form")
	m.base = baseList
	m.clearSurfaces()

	m.rail.Focus(session)
	m.openQuickMode()
	frame("the quick bar")
	m.closeQuick()

	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: session})
	m.runRemoteCmd(t, cmd)
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the kill confirm", m.screenID())
	}
	frame("the confirm")
	m.base = baseList
	m.clearSurfaces()

	fake.mu.Lock()
	fake.stderr, fake.code = "denied"+hostile, 1
	fake.mu.Unlock()
	m.ssh.Preview = uiconnections.Preview{}
	m.applyTestMsg(t, m.syncRemotePreview()())
	frame("a failed read")

	m.applyTestMsg(t, m.ssh.PollHost("box")())
	setHost(m, "box", uilink.Facts(remote.HostState{Snapshot: raw, Failures: 1, Err: errors.New("box: denied" + hostile)}))
	m.rail.Focus(boxRow)
	frame("an offline host")
}
