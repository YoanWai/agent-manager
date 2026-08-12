package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// notifyRecorder stands in for desktop delivery and logs every ping.
type notifyRecorder struct {
	calls [][2]string
}

func (r *notifyRecorder) fn() func(string, string) {
	return func(title, body string) {
		r.calls = append(r.calls, [2]string{title, body})
	}
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
	if len(rec.calls) != 2 {
		t.Fatalf("want 2 notifications, got %v", rec.calls)
	}
	if rec.calls[0] != [2]string{sess.Name, "Waiting for your input"} {
		t.Fatalf("unexpected waiting notification %v", rec.calls[0])
	}
	if rec.calls[1] != [2]string{sess.Name, "Errored"} {
		t.Fatalf("unexpected errored notification %v", rec.calls[1])
	}
}

func TestNotifyTransitionSkipsUnattentionStatuses(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	for _, st := range []string{status.Working, status.Idle, status.Dead, status.Starting} {
		p.notifyTransition(sess, st)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("no notification should fire for working/idle/dead/starting, got %v", rec.calls)
	}
}

// Finished is routine for most turn ends, so it only pings when the user
// opted in.
func TestNotifyTransitionFinishedOptIn(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	p.notifyTransition(sess, status.Finished)
	if len(rec.calls) != 0 {
		t.Fatalf("finished should stay quiet by default, got %v", rec.calls)
	}
	if err := p.store.SetSetting(notifyFinishedSetting, "on"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Finished)
	if len(rec.calls) != 1 || rec.calls[0] != [2]string{sess.Name, "Finished"} {
		t.Fatalf("want one finished notification after opt-in, got %v", rec.calls)
	}
}

func TestNotifyTransitionSilencedBySetting(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	if err := p.store.SetSetting(notificationsSetting, "off"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Waiting)
	p.notifyTransition(sess, status.Errored)
	if len(rec.calls) != 0 {
		t.Fatalf("notifications off should silence everything, got %v", rec.calls)
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
	if len(rec.calls) != 1 || rec.calls[0] != [2]string{"needy", "Waiting for your input"} {
		t.Fatalf("want one waiting notification titled with the session name, got %v", rec.calls)
	}

	m.applyCmd(t, m.refreshCmd())
	if len(rec.calls) != 1 {
		t.Fatalf("a steady waiting status should not re-fire, got %v", rec.calls)
	}
}
