package ui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

type notifyRecorder struct {
	mu    sync.Mutex
	calls [][2]string
}

func (r *notifyRecorder) fn() func(string, string) {
	return func(title, body string) {
		r.mu.Lock()
		r.calls = append(r.calls, [2]string{title, body})
		r.mu.Unlock()
	}
}

func (r *notifyRecorder) all() [][2]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][2]string(nil), r.calls...)
}

// waitForCalls blocks until n notifications have landed. Delivery runs off
// the refresh path, so assertions have to give it a moment.
func waitForCalls(t *testing.T, rec *notifyRecorder, n int) [][2]string {
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
	if calls[0] != [2]string{sess.Name, "Waiting for your input"} {
		t.Fatalf("unexpected waiting notification %v", calls[0])
	}
	if calls[1] != [2]string{sess.Name, "Errored"} {
		t.Fatalf("unexpected errored notification %v", calls[1])
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
	if calls[0] != [2]string{sess.Name, "Finished"} {
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
	if calls[0] != [2]string{"needy", "Waiting for your input"} {
		t.Fatalf("want one waiting notification titled with the session name, got %v", calls)
	}

	m.applyCmd(t, m.refreshCmd())
	settle()
	if calls := rec.all(); len(calls) != 1 {
		t.Fatalf("a steady waiting status should not re-fire, got %v", calls)
	}
}
