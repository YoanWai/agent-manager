package ui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

type notifyRecorder struct {
	mu    sync.Mutex
	calls []notify.Event
}

func (r *notifyRecorder) fn() func(notify.Event) {
	return func(event notify.Event) {
		r.mu.Lock()
		r.calls = append(r.calls, event)
		r.mu.Unlock()
	}
}

func (r *notifyRecorder) all() []notify.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notify.Event(nil), r.calls...)
}

// waitForCalls blocks until n notifications have landed. Delivery runs off
// the refresh path, so assertions have to give it a moment.
func waitForCalls(t *testing.T, rec *notifyRecorder, n int) []notify.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		calls := rec.all()
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d notifications, got %v", n, calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// settle gives a delivery that should NOT happen a window to arrive before
// asserting the recorder stayed empty.
func settle() {
	time.Sleep(100 * time.Millisecond)
}

func newNotifyTestPoller(t *testing.T) (*poller, store.Session, *notifyRecorder) {
	t.Helper()
	p, sess := newTestPollerWithSession(t)
	rec := &notifyRecorder{}
	p.notifyFn = rec.fn()
	return p, sess, rec
}

func TestNotifyTransitionFiresOnWaitingAndErrored(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	p.notifyTransition(sess, status.Waiting)
	p.notifyTransition(sess, status.Errored)
	calls := waitForCalls(t, rec, 2)
	want := map[notify.Event]bool{
		{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Waiting}: true,
		{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Errored}: true,
	}
	for _, call := range calls {
		delete(want, call)
	}
	if len(want) != 0 {
		t.Fatalf("missing notifications %v, got %v", want, calls)
	}
}

func TestNotifyTransitionCarriesCustomToolName(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	sess.Tool = "my-custom-agent"
	p.notifyTransition(sess, status.Waiting)
	calls := waitForCalls(t, rec, 1)
	if calls[0] != (notify.Event{ID: sess.ID, Session: sess.Name, Tool: "my-custom-agent", Kind: notify.Waiting}) {
		t.Fatalf("configured tool identity should reach the backend, got %v", calls)
	}
}

func TestNotifyTransitionSkipsUnattentionStatuses(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	for _, st := range []string{status.Working, status.Idle, status.Dead, status.Starting} {
		p.notifyTransition(sess, st)
	}
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("no notification should fire for working/idle/dead/starting, got %v", calls)
	}
}

// Finished is routine for most turn ends, so it only pings when the user
// opted in.
func TestNotifyTransitionFinishedOptIn(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	p.notifyTransition(sess, status.Finished)
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("finished should stay quiet by default, got %v", calls)
	}
	if err := p.store.SetSetting(notifyFinishedSetting, "on"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Finished)
	calls := waitForCalls(t, rec, 1)
	if calls[0] != (notify.Event{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Finished}) {
		t.Fatalf("want one finished notification after opt-in, got %v", calls)
	}
}

func TestNotifyTransitionSilencedBySetting(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	if err := p.store.SetSetting(notificationsSetting, "off"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Waiting)
	p.notifyTransition(sess, status.Errored)
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("notifications off should silence everything, got %v", calls)
	}
}

// The full path a waiting transition travels in the running app: hooks
// file → refreshOnce → store update → notification. Fires once; a status
// that stays waiting across polls never re-fires.
func TestRefreshNotifiesWaitingTransitionOnce(t *testing.T) {
	m := buildModel(t)
	hooked := m.cfg.Tools["claude-hooked"]
	hooked.Command = `sh -c 'exec cat' --`
	m.cfg.Tools["claude-hooked"] = hooked
	m.openForm()
	m.form.name.SetValue("needy")
	m.form.dir.SetValue(t.TempDir())
	for i, name := range m.form.toolNames {
		if name == "claude-hooked" {
			m.form.toolIndex = i
		}
	}
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	sess := m.sessionRows()[0]
	waitForPane(t, m, sess.ID, "boot-marker")

	// Pin the pre-state: whatever earlier refreshes derived, the
	// transition into waiting is observed by the next refresh and only
	// there.
	if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}

	rec := &notifyRecorder{}
	m.poller.notifyFn = rec.fn()

	statusFile := m.poller.hooks.StatusFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(statusFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusFile, []byte(status.Waiting), 0o644); err != nil {
		t.Fatal(err)
	}

	m.applyCmd(t, m.refreshCmd())
	calls := waitForCalls(t, rec, 1)
	if calls[0] != (notify.Event{ID: sess.ID, Session: "needy", Tool: "claude-hooked", Kind: notify.Waiting}) {
		t.Fatalf("want one waiting notification titled with the session name, got %v", calls)
	}

	m.applyCmd(t, m.refreshCmd())
	settle()
	if calls := rec.all(); len(calls) != 1 {
		t.Fatalf("a steady waiting status should not re-fire, got %v", calls)
	}
}

// A click on a banner leaves the session id in the config directory; the
// next pass picks it up and moves the cursor to that row.
func TestRefreshSelectsSessionNamedByClickedNotification(t *testing.T) {
	m := buildModel(t)
	createSessionOn(t, m, "first", "quietchat", t.TempDir())
	createSessionOn(t, m, "second", "quietchat", t.TempDir())
	m.selectSessionRow(t, "first")
	var second store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "second" {
			second = sess
		}
	}
	if second.ID == "" {
		t.Fatal("second session missing")
	}
	served := false
	m.poller.takeFocus = func() (string, bool) {
		if served {
			return "", false
		}
		served = true
		return second.ID, true
	}
	m.applyCmd(t, m.refreshCmd())
	if sess, ok := m.selected(); !ok || sess.ID != second.ID {
		t.Fatalf("the click should select the named session, cursor is on %+v", sess)
	}
}

// The keyboard is inside a pane in focus mode, so moving the cursor under
// it would leave every keystroke going to the session the user left.
func TestClickedNotificationLeavesFocusBeforeSelecting(t *testing.T) {
	m := buildModel(t)
	createSessionOn(t, m, "typing", "quietchat", t.TempDir())
	createSessionOn(t, m, "waiting", "quietchat", t.TempDir())
	m.selectSessionRow(t, "typing")
	var other store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "waiting" {
			other = sess
		}
	}
	if other.ID == "" {
		t.Fatal("waiting session missing")
	}
	m.mode = modeFocus
	served := false
	m.poller.takeFocus = func() (string, bool) {
		if served {
			return "", false
		}
		served = true
		return other.ID, true
	}
	m.applyCmd(t, m.refreshCmd())
	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list", m.mode)
	}
	if sess, ok := m.selected(); !ok || sess.ID != other.ID {
		t.Fatalf("cursor is on %+v, want the session the banner named", sess)
	}
}

// A banner outlives its session: the row can be gone by the time the user
// clicks it, and that must cost neither the cursor nor the focused pane.
func TestClickedNotificationForAGoneSessionChangesNothing(t *testing.T) {
	for _, test := range []struct {
		name string
		mode mode
	}{
		{"list", modeList},
		{"focus", modeFocus},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := buildModel(t)
			createSessionOn(t, m, "still-here", "quietchat", t.TempDir())
			m.selectSessionRow(t, "still-here")
			before, ok := m.selected()
			if !ok {
				t.Fatal("nothing selected")
			}
			m.mode = test.mode
			served := false
			m.poller.takeFocus = func() (string, bool) {
				if served {
					return "", false
				}
				served = true
				return "sess-long-gone", true
			}
			m.applyCmd(t, m.refreshCmd())
			if m.mode != test.mode {
				t.Fatalf("mode = %v, want %v", m.mode, test.mode)
			}
			if sess, ok := m.selected(); !ok || sess.ID != before.ID {
				t.Fatalf("cursor moved to %+v, want it left on %s", sess, before.Name)
			}
		})
	}
}
