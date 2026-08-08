package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestStatusFilterAttentionMatches(t *testing.T) {
	want := map[string]bool{
		status.Waiting:  true,
		status.Finished: true,
		status.Errored:  true,
		status.Working:  false,
		status.Idle:     false,
		status.Dead:     false,
		status.Starting: false,
	}
	for st, keep := range want {
		if got := statusFilterAttention.matches(st); got != keep {
			t.Fatalf("attention.matches(%q) = %v want %v", st, got, keep)
		}
	}
	if !statusFilterAll.matches(status.Idle) {
		t.Fatal("all filter should match every status")
	}
}

func TestStatusFilterCycleStartsAtAttention(t *testing.T) {
	if got := statusFilterAll.next(); got != statusFilterAttention {
		t.Fatalf("first w press = %v want attention", got)
	}
	if got := statusFilterAttention.next(); got != statusFilterAll {
		t.Fatalf("second w press = %v want all (only one mode for now)", got)
	}
	if statusFilterAll.active() {
		t.Fatal("all should not report active")
	}
	if !statusFilterAttention.active() {
		t.Fatal("attention should report active")
	}
	if statusFilterAttention.label() != "attention" {
		t.Fatalf("label = %q", statusFilterAttention.label())
	}
}

func TestStatusFilterKeyKeepsAttentionSessions(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "w", Name: "needs-you", Tool: "claude", Cwd: "/tmp", Status: status.Waiting},
		{ID: "f", Name: "done-turn", Tool: "claude", Cwd: "/tmp", Status: status.Finished},
		{ID: "e", Name: "broke", Tool: "claude", Cwd: "/tmp", Status: status.Errored},
		{ID: "busy", Name: "grinding", Tool: "claude", Cwd: "/tmp", Status: status.Working},
		{ID: "rest", Name: "quiet", Tool: "claude", Cwd: "/tmp", Status: status.Idle},
		{ID: "gone", Name: "killed", Tool: "claude", Cwd: "/tmp", Status: status.Dead},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if m.statusFilter != statusFilterAttention {
		t.Fatalf("statusFilter = %v want attention", m.statusFilter)
	}
	got := sessionNames(m)
	want := []string{"needs-you", "done-turn", "broke"}
	if !slices.Equal(got, want) {
		t.Fatalf("attention list = %v want %v", got, want)
	}
	header := ansi.Strip(strings.Join(m.viewHeaderRows(), "\n"))
	if !strings.Contains(header, "ATTENTION") {
		t.Fatalf("header missing ATTENTION badge:\n%s", header)
	}
	if !strings.Contains(header, "3 agents") {
		t.Fatalf("header count should match the listed agents:\n%s", header)
	}
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "show all") {
		t.Fatalf("footer should offer clearing the filter:\n%s", footer)
	}

	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if m.statusFilter != statusFilterAll {
		t.Fatalf("second w should clear filter, got %v", m.statusFilter)
	}
	if got := sessionNames(m); len(got) != 6 {
		t.Fatalf("cleared filter should show all 6 sessions, got %v", got)
	}
}

func TestStatusFilterIgnoresFolds(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, sess := range []store.Session{
		{ID: "wait", Name: "blocked", Tool: "claude", Cwd: "/tmp", Group: "work", Status: status.Waiting},
		{ID: "idle", Name: "resting", Tool: "claude", Cwd: "/tmp", Group: "work", Status: status.Idle},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.collapsed["work"] = true
	m.rebuildRows()
	if len(m.sessionRows()) != 0 {
		t.Fatalf("fold should hide sessions before filter, got %d", len(m.sessionRows()))
	}

	m.statusFilter = statusFilterAttention
	m.rebuildRows()
	got := sessionNames(m)
	if !slices.Equal(got, []string{"blocked"}) {
		t.Fatalf("attention filter should reveal the waiting session past the fold, got %v", got)
	}
}

func TestStatusFilterEmptyState(t *testing.T) {
	m := shotModel()
	m.sessions = []store.Session{
		{ID: "idle", Name: "quiet", Tool: "claude", Cwd: "/tmp", Status: status.Idle},
	}
	m.statusFilter = statusFilterAttention
	m.rebuildRows()
	rail := ansi.Strip(strings.Join(splitLines(joinContentText(m.railLines(40, 20))), "\n"))
	if !strings.Contains(rail, "nothing needs attention") {
		t.Fatalf("rail missing empty attention copy:\n%s", rail)
	}
}

func TestStatusFilterGroupRosterMatchesCount(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("fleet", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, sess := range []store.Session{
		{ID: "w", Name: "needs-you", Tool: "claude", Cwd: "/tmp", Group: "fleet", Status: status.Waiting},
		{ID: "busy", Name: "grinding", Tool: "claude", Cwd: "/tmp", Group: "fleet", Status: status.Working},
		{ID: "rest", Name: "quiet", Tool: "claude", Cwd: "/tmp", Group: "fleet", Status: status.Idle},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.statusFilter = statusFilterAttention
	m.rebuildRows()

	if got := m.groupSessionCount("fleet"); got != 1 {
		t.Fatalf("group count under attention = %d want 1", got)
	}
	roster := ansi.Strip(m.viewGroupAgents("fleet", 60, 20))
	if !strings.Contains(roster, "needs-you") {
		t.Fatalf("roster missing waiting session:\n%s", roster)
	}
	if strings.Contains(roster, "grinding") || strings.Contains(roster, "quiet") {
		t.Fatalf("roster should only list attention sessions:\n%s", roster)
	}
}

func TestBulkActionsRespectStatusFilter(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "w", Name: "needs-you", Tool: "claude", Cwd: "/tmp", Status: status.Waiting},
		{ID: "busy", Name: "grinding", Tool: "claude", Cwd: "/tmp", Status: status.Working},
		{ID: "gone", Name: "killed", Tool: "claude", Cwd: "/tmp", Status: status.Dead},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	for _, id := range []string{"w", "busy"} {
		if err := m.tmux.Create(id, t.TempDir(), "cat", nil, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	m.statusFilter = statusFilterAttention
	m.rebuildRows()

	updated, _ := m.killAllLive()
	m = updated.(*Model)
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].ID != "w" {
		t.Fatalf("filtered kill-all targets = %+v, want only the listed session", m.confirm.sessions)
	}
	m.mode = modeList

	if got := m.sessionsInGroup(""); len(got) != 1 || got[0].ID != "w" {
		t.Fatalf("filtered group sessions = %+v, want only the listed session", got)
	}

	updated, _ = m.reviveAllDead()
	m = updated.(*Model)
	if m.errBar.text != "no dead sessions to revive" {
		t.Fatalf("filtered revive-all touched hidden sessions: %q", m.errBar.text)
	}
}

func TestAttentionFilterKeepsSelectedAfterAck(t *testing.T) {
	m := attentionFilterAckFixture(t)
	m.selectSessionRow(t, "just-finished")

	sess, ok := m.selected()
	if !ok {
		t.Fatal("expected a selected session")
	}
	if err := m.store.AcknowledgeFinished(sess.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	loadStoredRows(t, m)

	if got := sessionNames(m); !slices.Equal(got, []string{"just-finished", "still-waiting"}) {
		t.Fatalf("attention list after ack = %v want both sessions still listed", got)
	}
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || entry.sess.Name != "just-finished" {
		t.Fatalf("cursor should stay on the acked session, got %+v", entry)
	}
	if entry.sess.Status != status.Idle {
		t.Fatalf("acked row status = %q want idle", entry.sess.Status)
	}
}

func TestAttentionFilterDropsAckedAfterMove(t *testing.T) {
	m := attentionFilterAckFixture(t)
	m.selectSessionRow(t, "just-finished")

	sess, ok := m.selected()
	if !ok {
		t.Fatal("expected a selected session")
	}
	if err := m.store.AcknowledgeFinished(sess.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "still-waiting")
	loadStoredRows(t, m)

	if got := sessionNames(m); !slices.Equal(got, []string{"still-waiting"}) {
		t.Fatalf("attention list after move = %v want only the still-matching session", got)
	}
	if entry, ok := m.selectedRow(); !ok || entry.isGroup || entry.sess.Name != "still-waiting" {
		t.Fatalf("cursor should stay on the waiting session, got %+v", entry)
	}
}

func attentionFilterAckFixture(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "done", Name: "just-finished", Tool: "claude", Cwd: "/tmp", Status: status.Finished},
		{ID: "wait", Name: "still-waiting", Tool: "claude", Cwd: "/tmp", Status: status.Waiting},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.statusFilter = statusFilterAttention
	m.rebuildRows()
	return m
}

func TestForkClearsStatusFilter(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "source", dir, "")
	m.selectSessionRow(t, "source")
	source := m.rows[m.cursor].sess
	if err := m.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.sessions {
		if m.sessions[i].ID == source.ID {
			m.sessions[i].AgentSessionID = "source-conversation"
			m.sessions[i].Status = status.Waiting
		}
	}
	m.statusFilter = statusFilterAttention
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {id}; cat"
	m.cfg.Tools[source.Tool] = tool

	m.openFork()
	m.fork.name.SetValue("fork-under-filter")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if m.statusFilter != statusFilterAll {
		t.Fatalf("fork should clear the filter, got %v", m.statusFilter)
	}
	if entry, ok := m.selectedRow(); !ok || entry.isGroup || entry.sess.Name != "fork-under-filter" {
		t.Fatalf("cursor should land on the forked row, got %+v", entry)
	}
}
