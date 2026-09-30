package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

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
	m.poller.dependencies.TakeFocus = func() (string, bool) {
		if served {
			return "", false
		}
		served = true
		return second.ID, true
	}
	resetExecution(m)
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
	m.poller.dependencies.TakeFocus = func() (string, bool) {
		if served {
			return "", false
		}
		served = true
		return other.ID, true
	}
	resetExecution(m)
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
			m.poller.dependencies.TakeFocus = func() (string, bool) {
				if served {
					return "", false
				}
				served = true
				return "sess-long-gone", true
			}
			resetExecution(m)
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
