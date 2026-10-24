package link

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// ssh stands in for ssh: it answers as the remote CLI would and records the
// words each call carried.
type ssh struct {
	mu       sync.Mutex
	calls    [][]string
	snapshot sessioncmd.Snapshot
	screen   string
	err      error
}

func (f *ssh) run(ctx context.Context, argv []string) ([]byte, []byte, error) {
	words, err := remotetest.Words(argv)
	if err != nil {
		return nil, nil, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, words[1:])
	snapshot, screen, fail := f.snapshot, f.screen, f.err
	f.mu.Unlock()
	if fail != nil {
		return nil, nil, fail
	}
	var reply any
	switch {
	case words[0] == "snapshot":
		reply = snapshot
	case words[0] == "read":
		reply = sessioncmd.SessionScreen{Session: sessioncmd.Session{ID: words[len(words)-1]}, Output: screen}
	case words[0] == "terminal" && words[1] == "read":
		reply = sessioncmd.TerminalScreen{Terminal: sessioncmd.Terminal{ID: words[len(words)-1]}, Output: screen}
	case words[0] == "terminal":
		reply = sessioncmd.Terminal{ID: "t9", Name: "shell"}
	default:
		reply = sessioncmd.Session{ID: "s9", Name: "fresh"}
	}
	out, err := json.Marshal(reply)
	return remotetest.Answer(out), nil, err
}

func (f *ssh) taken() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func newClient(t *testing.T, fake *ssh) *remote.Client {
	t.Helper()
	return remote.New(t.TempDir(), remote.WithRunner(fake.run))
}

func listedState(t *testing.T, fake *ssh) State {
	t.Helper()
	return New(newClient(t, fake), []store.Connection{{Name: "box", Destination: "me@box"}})
}

// recorder is the root as a routed result reaches it.
type recorder struct {
	rebuilt  int
	synced   int
	rail     []RailChange
	answered []bool
	closed   []Asker
	cleared  int
	errs     []string
	done     []string
}

func (r *recorder) RebuildRows() { r.rebuilt++ }

func (r *recorder) SyncPreview() tea.Cmd { r.synced++; return nil }

func (r *recorder) RailConnection(change RailChange) { r.rail = append(r.rail, change) }

func (r *recorder) AnswerSave(gen uint64, failed bool) { r.answered = append(r.answered, failed) }

func (r *recorder) CloseAsker(gen int, asker Asker) { r.closed = append(r.closed, asker) }

func (r *recorder) ClearAccepted(composerGen int, draft string, closeAfterSend bool) { r.cleared++ }

func (r *recorder) ReportErr(text string) { r.errs = append(r.errs, text) }

func (r *recorder) ReportDone(text string) { r.done = append(r.done, text) }

func TestPollHostRunsOffTheUpdatePathAndLandsTheRefresh(t *testing.T) {
	fake := &ssh{snapshot: sessioncmd.Snapshot{
		Version: sessioncmd.SnapshotVersion, ManagerAwake: true,
		Sessions: []sessioncmd.Session{{ID: "s1", Name: "api", Tool: "claude", Group: "web", Status: "idle"}},
	}}
	state := listedState(t, fake)
	cmd := state.PollHost("box")
	if cmd == nil || len(fake.taken()) != 0 {
		t.Fatal("the refresh ran on the update path")
	}
	if again := state.PollHost("box"); again != nil {
		t.Fatal("a second refresh started while the first was in flight")
	}
	root := &recorder{}
	if _, ok := state.Route(root, cmd()); !ok {
		t.Fatal("a refresh result went unrouted")
	}
	if root.rebuilt != 1 || root.synced != 1 {
		t.Fatalf("rebuilt %d, synced %d, want one each", root.rebuilt, root.synced)
	}
	if hosts := state.RailHosts(); len(hosts) != 1 || hosts[0].State != uirail.HostOnline || len(hosts[0].Sessions) != 1 {
		t.Fatalf("rail hosts = %+v", hosts)
	}
}

func TestARefreshOfAReplacedListIsDropped(t *testing.T) {
	fake := &ssh{}
	state := listedState(t, fake)
	stale := state.PollHost("box")
	state.SetList([]store.Connection{{Name: "box", Destination: "me@other"}})
	root := &recorder{}
	state.Route(root, stale())
	if _, seen := state.Host("box"); seen {
		t.Fatal("a refresh of the old destination landed on the new one")
	}
	if root.rebuilt != 0 {
		t.Fatal("the rail was rebuilt for a refresh that did not land")
	}
}

// A host that failed says why agent-manager there is unusable rather than
// reading as merely down.
func TestRailHostsNameTheTrouble(t *testing.T) {
	state := listedState(t, &ssh{})
	state.Refreshed("box", state.Gen(), Facts(remote.HostState{Failures: 1, Err: context.DeadlineExceeded}))
	host := state.RailHosts()[0]
	if host.State != uirail.HostOffline || host.Trouble != "unreachable" {
		t.Fatalf("host = %+v", host)
	}
	if host.Err == "" {
		t.Fatal("the host's error was dropped")
	}
}

func TestPreviewReadsTheTrackedRowAndDropsAStaleRead(t *testing.T) {
	fake := &ssh{screen: "building api"}
	state := listedState(t, fake)
	state.Refreshed("box", state.Gen(), Facts(remote.HostState{OK: true, Snapshot: sessioncmd.Snapshot{
		Sessions: []sessioncmd.Session{{ID: "s1", Name: "api", Status: "idle"}},
	}}))
	row, ok := state.Row(uirail.Selection{Kind: uirail.SessionRow, Host: "box", SessionID: "s1"})
	if !ok {
		t.Fatal("the session has no row")
	}
	root := &recorder{}
	cmd := state.Follow(row, true)
	if cmd == nil {
		t.Fatal("the selected session got no read")
	}
	if state.Follow(row, true) != nil {
		t.Fatal("a second read started for the same row")
	}
	state.Route(root, cmd())
	if lines := state.Preview.Screen(40, 4, screenView()); !slices.Equal(lines, []string{"building api"}) {
		t.Fatalf("screen = %q", lines)
	}
	stale, ok := state.StartRead()
	if !ok {
		t.Fatal("the tracked row refused a read")
	}
	state.Preview.Follow(row, false)
	state.Route(root, PreviewMsg{Read: stale, Text: "old screen"})
	if lines := state.Preview.Screen(40, 4, screenView()); slices.Contains(lines, "old screen") {
		t.Fatalf("a read for the row before the selection moved landed: %q", lines)
	}
}

func TestAttachBuildsTheCommandOffTheUpdatePath(t *testing.T) {
	state := listedState(t, &ssh{})
	row := uiconnections.Row{Host: "box", Ref: remote.Ref{Host: "box", ID: "s1"}}
	msg, ok := state.Attach(row)().(AttachReadyMsg)
	if !ok || msg.Err != nil || msg.Host != "box" || msg.Cmd == nil {
		t.Fatalf("attach = %+v", msg)
	}
	root := &recorder{}
	if cmd, routed := state.Route(root, msg); !routed || cmd == nil {
		t.Fatal("the attach was not handed to the terminal")
	}
	if len(root.errs) != 0 {
		t.Fatalf("errors %q", root.errs)
	}
}

func TestAttachEndingNamesWhy(t *testing.T) {
	state := listedState(t, &ssh{})
	root := &recorder{}
	state.Route(root, AttachDoneMsg{Host: "box", Err: exitStatus(120)})
	want := "review opens only for sessions on this machine, not yet for one on box"
	if len(root.errs) != 1 || root.errs[0] != want {
		t.Fatalf("errors = %q, want %q", root.errs, want)
	}
}

// screenView is the preview's painter with the tab expansion the root
// supplies.
func screenView() uiconnections.ScreenView {
	return uiconnections.ScreenView{ExpandTabs: func(line string, width int) string { return line }}
}

type exitStatus int

func (e exitStatus) Error() string { return "exit status" }
func (e exitStatus) ExitCode() int { return int(e) }
