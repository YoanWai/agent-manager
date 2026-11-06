package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uiinbox "github.com/YoanWai/agent-manager/internal/ui/inbox"
	"github.com/charmbracelet/x/ansi"
)

func inboxSurface(t *testing.T, m *Model) *uiinbox.Surface {
	t.Helper()
	s, ok := surfaceOf[*uiinbox.Surface](m)
	if !ok {
		t.Fatalf("no inbox is open, screen = %v", m.screenID())
	}
	return s
}

func queueFor(t *testing.T, m *Model, to, from store.Session, subject, body string) {
	t.Helper()
	if _, err := m.services.store.Enqueue(store.InboxMessage{
		SessionID: to.ID, SenderID: from.ID, SenderName: from.Name, Subject: subject,
		Body: body, Fingerprint: body, SentAt: time.Now(),
	}, store.DefaultInboxLimits); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

func TestInboxKeyOpensTheSelectedSessionsMessages(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	alpha, beta := sessionRow(t, m, "alpha").sess, sessionRow(t, m, "beta").sess
	queueFor(t, m, alpha, beta, "review report", "two findings\nin detail")
	m.selectSessionRow(t, "alpha")

	m.Update(key("i"))
	if s := inboxSurface(t, m); s.SessionID() != alpha.ID {
		t.Fatalf("the key opened %s's inbox, want alpha's", s.SessionID())
	}
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "Loading messages") {
		t.Fatalf("before its load the inbox shows:\n%s", view)
	}
	m.drainEffects(t)
	view := ansi.Strip(preparedView(m))
	if !strings.Contains(view, "Inbox: alpha") || !strings.Contains(view, "Waiting (1)") ||
		!strings.Contains(view, "beta → alpha") || !strings.Contains(view, "review report") {
		t.Fatalf("the inbox does not list the queued message:\n%s", view)
	}

	m.Update(key("enter"))
	m.drainEffects(t)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "two findings") || !strings.Contains(view, "in detail") {
		t.Fatalf("enter did not show the whole message:\n%s", view)
	}
	m.Update(key("esc"))
	m.Update(key("esc"))
	if _, open := surfaceOf[*uiinbox.Surface](m); open || !m.onList() {
		t.Fatalf("esc twice did not close the inbox, screen = %v", m.screenID())
	}
}

func TestClickingTheInboxBadgeOpensThatRowsMessages(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	alpha, beta := sessionRow(t, m, "alpha").sess, sessionRow(t, m, "beta").sess
	queueFor(t, m, beta, alpha, "", "rebase on main")
	m.workspace.queuedMessages = map[string]int{beta.ID: 1}
	m.rebuildRows()
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "beta")[0]
	badge, ok := m.layout.displayedRail.Inboxes["s:"+beta.ID]
	if !ok {
		t.Fatal("beta's badge recorded no hit area")
	}
	y0, _ := m.bodyYRange()
	m = leftClick(m, badge.X, y0+line)
	if s := inboxSurface(t, m); s.SessionID() != beta.ID {
		t.Fatalf("the badge opened %s's inbox, want beta's", s.SessionID())
	}
	m.drainEffects(t)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "rebase on main") {
		t.Fatalf("the inbox does not show the first line as the subject:\n%s", view)
	}
}

func TestTheOpenInboxReloadsWhenItsQueueMoves(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	alpha, beta := sessionRow(t, m, "alpha").sess, sessionRow(t, m, "beta").sess
	m.selectSessionRow(t, "alpha")
	m.Update(key("i"))
	m.drainEffects(t)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "No messages to or from this session") {
		t.Fatalf("an empty inbox shows:\n%s", view)
	}

	queueFor(t, m, alpha, beta, "fresh news", "fresh news body")
	previous := m.workspace.queuedMessages
	m.workspace.queuedMessages = map[string]int{alpha.ID: 1}
	m.followInbox(previous)
	m.drainEffects(t)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "fresh news") {
		t.Fatalf("a new queued message did not reach the open inbox:\n%s", view)
	}
}

func TestInboxFitsANarrowTerminal(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	alpha, beta := sessionRow(t, m, "alpha").sess, sessionRow(t, m, "beta").sess
	queueFor(t, m, alpha, beta, strings.Repeat("long subject ", 8), "body")
	m.layout.width, m.layout.height = 40, 24
	m.selectSessionRow(t, "alpha")
	m.Update(key("i"))
	m.drainEffects(t)
	for _, line := range strings.Split(preparedView(m), "\n") {
		if width := ansi.StringWidth(line); width > 40 {
			t.Fatalf("a line is %d wide on a 40 column terminal: %q", width, ansi.Strip(line))
		}
	}
}
