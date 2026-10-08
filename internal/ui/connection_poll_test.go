package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
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
		reply = sessioncmd.SessionScreen{Output: screen}
	case "terminal":
		switch words[1] {
		case "close":
			return []byte("closed\n"), nil, nil
		case "read":
			reply = sessioncmd.TerminalScreen{Output: screen}
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

// connectedModel lists one connection, box, as its last refresh left it,
// with every call going to fake.
func connectedModel(t *testing.T, fake *fakeSSH) *Model {
	t.Helper()
	m := buildModel(t)
	fake.snapshot = boxSnapshot()
	m.ssh = newConnections(remote.New(t.TempDir(), remote.WithRunner(fake.run)), []store.Connection{{Name: "box", Destination: "me@box"}})
	m.ssh.hosts["box"] = remote.HostState{OK: true, Snapshot: fake.snapshot}
	m.rebuildRows()
	return m
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
	m := connectedModel(t, fake)
	delete(m.ssh.hosts, "box")
	m.rebuildRows()

	cmd := m.pollConnection("box")
	if cmd == nil || !m.ssh.polling["box"] {
		t.Fatal("no refresh started")
	}
	if len(fake.taken()) != 0 {
		t.Fatal("the refresh ran on the update path")
	}
	if again := m.pollConnection("box"); again != nil {
		t.Fatal("a second refresh started while the first was in flight")
	}
	m.applyTestMsg(t, cmd())
	if calls := fake.taken(); len(calls) != 1 || calls[0][0] != "snapshot" {
		t.Fatalf("calls = %q, want one snapshot", calls)
	}
	if m.ssh.polling["box"] || !m.ssh.hosts["box"].OK {
		t.Fatalf("polling %v state %+v after the refresh landed", m.ssh.polling, m.ssh.hosts["box"])
	}
	if names := railNames(m); !slices.Contains(names, "api") || !slices.Contains(names, "box") {
		t.Fatalf("rows = %q, want box and its agents", names)
	}
}

func TestOfflineHostKeepsItsLastRows(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.applyTestMsg(t, m.pollConnection("box")())
	fake.mu.Lock()
	fake.stderr, fake.code = "ssh: connect to host box port 22: Connection refused", 255
	fake.mu.Unlock()
	m.applyTestMsg(t, m.pollConnection("box")())

	state := m.ssh.hosts["box"]
	if state.OK || state.Failures != 1 || !strings.Contains(state.Err.Error(), "Connection refused") {
		t.Fatalf("state = %+v", state)
	}
	if names := railNames(m); !slices.Contains(names, "api") || !slices.Contains(names, "sh") {
		t.Fatalf("rows = %q, want the last snapshot kept", names)
	}
	if host := m.ssh.railHosts()[0]; host.State != uirail.HostOffline {
		t.Fatalf("rail host = %+v, want offline", host)
	}
}

func TestRefreshOfAReplacedListIsDropped(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	delete(m.ssh.hosts, "box")
	stale := m.pollConnection("box")
	m.ssh.setList([]store.Connection{{Name: "box", Destination: "me@other"}})
	m.applyTestMsg(t, stale())
	if _, seen := m.ssh.hosts["box"]; seen {
		t.Fatal("a refresh of the old destination landed on the new one")
	}
}

func TestRemovedConnectionTakesItsRowsWithIt(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.ssh.setList(nil)
	m.rebuildRows()
	if names := railNames(m); slices.Contains(names, "box") || slices.Contains(names, "api") {
		t.Fatalf("rows = %q after removal", names)
	}
}
