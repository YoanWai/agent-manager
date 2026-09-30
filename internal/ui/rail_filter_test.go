package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"
)

func TestToggleEmptyGroupsFiltersTreeWithoutDeletingGroups(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"empty", "work", "work/leaf", "work/unused"} {
		if err := m.services.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "nested", t.TempDir(), "work/leaf")

	m.selectGroupRow(t, "empty")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd != nil {
		m.applyCmd(t, cmd)
	}

	wantVisible := []string{"work", "work/leaf"}
	if got := m.groupRowPaths(); !slices.Equal(got, wantVisible) {
		t.Fatalf("groups with empty subtrees should be hidden, got %v want %v", got, wantVisible)
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "show empty") {
		t.Fatalf("footer should offer the inverse action while filtered:\n%s", footer)
	}
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("list stored groups: %v", err)
	}
	if len(groups) != 4 {
		t.Fatalf("visual filter changed stored groups: got %d want 4", len(groups))
	}

	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	wantAll := []string{"empty", "work", "work/leaf", "work/unused"}
	if got := m.groupRowPaths(); !slices.Equal(got, wantAll) {
		t.Fatalf("second toggle should restore empty groups, got %v want %v", got, wantAll)
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "hide empty") {
		t.Fatalf("footer should offer hiding while empty groups are visible:\n%s", footer)
	}
}

func TestHideEmptyGroupsDoesNotFilterTheArchivedView(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if err := m.services.store.CreateGroup("bare", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.rail.SetHideEmptyGroups(true)
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if got, want := m.groupRowPaths(), []string{"empty"}; !slices.Equal(got, want) {
		t.Fatalf("archived view with hide-empty on should keep the archived empty group, got %v want %v", got, want)
	}

	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	if got := m.groupRowPaths(); len(got) != 0 {
		t.Fatalf("active view with hide-empty on should still hide empty groups, got %v", got)
	}
}

func TestEmptyGroupsKeyIsRefusedInTheArchivedView(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, path := range []string{"empty", "work"} {
		if err := m.services.store.CreateGroup(path, ""); err != nil {
			t.Fatalf("create group %s: %v", path, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")

	press := func(key string) {
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil {
			m.applyCmd(t, cmd)
		}
		m.applyCmd(t, m.refreshCmd())
	}

	press("e")
	if got, want := m.groupRowPaths(), []string{"work"}; !slices.Equal(got, want) {
		t.Fatalf("e should hide the empty group, got %v want %v", got, want)
	}

	press("t")
	press("e")
	if !m.rail.HideEmptyGroups() {
		t.Fatal("e in the archived view should leave the hide-empty setting alone")
	}

	press("t")
	if got, want := m.groupRowPaths(), []string{"work"}; !slices.Equal(got, want) {
		t.Fatalf("back on the active list, empty groups should still be hidden, got %v want %v", got, want)
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
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if !m.rail.FilteringAttention() {
		t.Fatalf("statusFilter = %v want attention", m.rail.FilteringAttention())
	}
	got := sessionNames(m)
	want := []string{"needs-you", "done-turn", "broke"}
	if !slices.Equal(got, want) {
		t.Fatalf("attention list = %v want %v", got, want)
	}
	m.width, m.height = 120, 34
	rail := ansi.Strip(railLinesText(m.railLines(36, m.listBodyHeight())))
	if !strings.Contains(rail, "ATTENTION") {
		t.Fatalf("rail missing ATTENTION badge:\n%s", rail)
	}
	if !strings.Contains(rail, "show all") {
		t.Fatalf("rail badge should offer clearing the filter:\n%s", rail)
	}
	header := ansi.Strip(strings.Join(m.viewHeaderRows(), "\n"))
	if strings.Contains(header, "ATTENTION") {
		t.Fatalf("filter badges belong over the list, not in the header:\n%s", header)
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
	if m.rail.FilteringAttention() {
		t.Fatalf("second w should clear filter, got %v", m.rail.FilteringAttention())
	}
	if got := sessionNames(m); len(got) != 6 {
		t.Fatalf("cleared filter should show all 6 sessions, got %v", got)
	}
}

func TestStatusFilterIgnoresFolds(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, sess := range []store.Session{
		{ID: "wait", Name: "blocked", Tool: "claude", Cwd: "/tmp", Group: "work", Status: status.Waiting},
		{ID: "idle", Name: "resting", Tool: "claude", Cwd: "/tmp", Group: "work", Status: status.Idle},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.rail.SetCollapsed("work", true)
	m.rebuildRows()
	if len(m.sessionRows()) != 0 {
		t.Fatalf("fold should hide sessions before filter, got %d", len(m.sessionRows()))
	}

	m.rail.SetFilteringAttention(true)
	m.rebuildRows()
	got := sessionNames(m)
	if !slices.Equal(got, []string{"blocked"}) {
		t.Fatalf("attention filter should reveal the waiting session past the fold, got %v", got)
	}
}

func TestStatusFilterEmptyState(t *testing.T) {
	m := shotModel()
	m.workspace.sessions = []store.Session{
		{ID: "idle", Name: "quiet", Tool: "claude", Cwd: "/tmp", Status: status.Idle},
	}
	m.rail.SetFilteringAttention(true)
	m.rebuildRows()
	rail := ansi.Strip(strings.Join(splitLines(joinContentText(m.railLines(40, 20))), "\n"))
	if !strings.Contains(rail, "nothing needs attention") {
		t.Fatalf("rail missing empty attention copy:\n%s", rail)
	}
}

func TestStatusFilterGroupRosterMatchesCount(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("fleet", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, sess := range []store.Session{
		{ID: "w", Name: "needs-you", Tool: "claude", Cwd: "/tmp", Group: "fleet", Status: status.Waiting},
		{ID: "busy", Name: "grinding", Tool: "claude", Cwd: "/tmp", Group: "fleet", Status: status.Working},
		{ID: "rest", Name: "quiet", Tool: "claude", Cwd: "/tmp", Group: "fleet", Status: status.Idle},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.rail.SetFilteringAttention(true)
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
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	for _, id := range []string{"w", "busy"} {
		if err := m.services.tmux.Create(id, t.TempDir(), "cat", nil, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	m.rail.SetFilteringAttention(true)
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

func TestReviveAllLeavesSessionsTheFilterHides(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, sess := range []store.Session{
		{ID: "shown", Name: "held-dead", Tool: "claude", Cwd: dir, Status: status.Dead},
		{ID: "hidden-a", Name: "filtered-a", Tool: "claude", Cwd: dir, Status: status.Dead},
		{ID: "hidden-b", Name: "filtered-b", Tool: "claude", Cwd: dir, Status: status.Dead},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	// The attention filter lists a dead session only while the cursor holds it.
	m.selectSessionRow(t, "held-dead")
	m.rail.SetFilteringAttention(true)
	m.rebuildRows()
	if got := sessionNames(m); !slices.Equal(got, []string{"held-dead"}) {
		t.Fatalf("filtered list = %v, want only the selected dead session", got)
	}

	updated, _ := m.reviveAllDead()
	m = updated.(*Model)
	if m.mode == modeConfirmDelete {
		t.Fatalf("hidden sessions were counted: %q", m.confirm.label)
	}
	if !m.services.tmux.Exists("shown") {
		t.Fatalf("the listed dead session should revive at once, err = %q", m.errBar.text)
	}
	for _, id := range []string{"hidden-a", "hidden-b"} {
		if m.services.tmux.Exists(id) {
			t.Fatalf("revive all brought back %s, which the filter hides", id)
		}
	}
}

func TestAttentionFilterKeepsSelectedAfterAck(t *testing.T) {
	m := attentionFilterAckFixture(t)
	m.selectSessionRow(t, "just-finished")

	sess, ok := m.selected()
	if !ok {
		t.Fatal("expected a selected session")
	}
	if err := m.services.store.AcknowledgeFinished(sess.ID); err != nil {
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
	if err := m.services.store.AcknowledgeFinished(sess.ID); err != nil {
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
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.rail.SetFilteringAttention(true)
	m.rebuildRows()
	return m
}

func TestForkClearsStatusFilter(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "source", dir, "")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
			m.workspace.sessions[i].Status = status.Waiting
		}
	}
	m.rail.SetFilteringAttention(true)
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {id}; cat"
	m.services.cfg.Tools[source.Tool] = tool

	m.openFork()
	m.fork.name.SetValue("fork-under-filter")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if m.rail.FilteringAttention() {
		t.Fatalf("fork should clear the filter, got %v", m.rail.FilteringAttention())
	}
	if entry, ok := m.selectedRow(); !ok || entry.isGroup || entry.sess.Name != "fork-under-filter" {
		t.Fatalf("cursor should land on the forked row, got %+v", entry)
	}
}
