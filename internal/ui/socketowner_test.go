package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
)

// guestPoller is a second manager reading the same store while talking to
// a tmux server of its own: the shape that stamped dead over every live
// session, so their own manager alerted the user on every flip back.
func guestPoller(t *testing.T, st *store.Store) *poller {
	t.Helper()
	driver, err := tmux.NewWithSocket("amguest" + strconv.FormatInt(time.Now().UnixNano(), 36))
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	return &poller{store: st, tmux: driver, hooks: hooks.NewManager(t.TempDir()), interval: time.Second}
}

func TestGuestManagerLeavesAnotherServersSessionsAlone(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "watched", t.TempDir(), "")
	sess := m.sessionRows()[0]
	m.poller.refreshOnce()
	before, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	guest := guestPoller(t, m.store)
	guest.refreshOnce()

	after, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status {
		t.Fatalf("guest manager rewrote status %q as %q", before.Status, after.Status)
	}
	if after.TmuxSocket != before.TmuxSocket {
		t.Fatalf("guest manager claimed the session: %q, want %q", after.TmuxSocket, before.TmuxSocket)
	}

	// The spam came from the owner reading the guest's dead stamp as a
	// transition on the next poll.
	rec := &notifyRecorder{}
	m.poller.notifyFn = rec.fn()
	m.poller.refreshOnce()
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("a poll beside a second manager should not alert, got %v", calls)
	}
}

func TestManagerStillMarksItsOwnSessionsDead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed-own", t.TempDir(), "")
	sess := m.sessionRows()[0]
	m.poller.refreshOnce()
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.poller.refreshOnce()

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Dead {
		t.Fatalf("status = %q, want dead once the pane on this server is gone", got.Status)
	}
}

func TestManagerClaimsUnstampedSessionsOnItsServer(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "unstamped", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.store.SetTmuxSocket(sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	m.poller.refreshOnce()

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TmuxSocket != m.tmux.SocketPath() {
		t.Fatalf("socket = %q, want this manager's %q", got.TmuxSocket, m.tmux.SocketPath())
	}
}

// Rows that predate the column have no server to compare against, so they
// belong to whichever manager holds the heartbeat.
func TestUnstampedSessionsFollowTheLeadingManager(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "legacy-1", Name: "legacy", Tool: "claude", Cwd: t.TempDir(), Status: status.Working}
	if err := m.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetTmuxSocket(sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetSetting(store.PollerSocketKey, "/tmp/another-manager/agentmgr"); err != nil {
		t.Fatal(err)
	}
	stampHeartbeat(t, m.store, time.Now())
	m.poller.heartbeatAt = time.Time{}
	m.poller.refreshOnce()

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Working {
		t.Fatalf("status = %q, want the leading manager's %q left alone", got.Status, status.Working)
	}

	// The claim runs on the heartbeat's cadence, so age this manager's own
	// stamp as well to reach the poll that takes the store over.
	stampHeartbeat(t, m.store, time.Now().Add(-2*store.PollerHeartbeatStale))
	m.poller.heartbeatAt = time.Time{}
	m.poller.refreshOnce()

	got, err = m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Dead {
		t.Fatalf("status = %q, want dead once no other manager is home", got.Status)
	}
}

func stampHeartbeat(t *testing.T, st *store.Store, at time.Time) {
	t.Helper()
	if err := st.SetSetting(store.PollerHeartbeatKey, strconv.FormatInt(at.UnixNano(), 10)); err != nil {
		t.Fatal(err)
	}
}

func TestRowMarksSessionsOnAnotherServer(t *testing.T) {
	now := time.Now()
	sess := store.Session{
		ID: "away-1", Name: "away", Tool: "claude", Status: status.Working,
		CreatedAt: now, LastStatusAt: now, TmuxSocket: "/tmp/another-manager/agentmgr",
	}
	m := &Model{
		width: 120, height: 40, mode: modeList,
		sessions: []store.Session{sess}, rows: []treeRow{{sess: sess}},
		collapsed: map[string]bool{}, split: splitState{ratio: defaultSplitRatio},
		tmuxSocket: "/tmp/tmux-501/agentmgr",
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "elsewhere") {
		t.Fatalf("a session on another server should say so:\n%s", view)
	}

	m.tmuxSocket = sess.TmuxSocket
	if view := ansi.Strip(m.View()); strings.Contains(view, "elsewhere") {
		t.Fatalf("a session on this server should not be marked:\n%s", view)
	}
}
