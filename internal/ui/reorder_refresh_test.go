package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestReorderSurvivesInFlightPoll(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	pollListed := slices.Clone(m.sessions)
	pollAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.store), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after first shift+down = %v want %v", got, want)
	}
	updated, _ = m.Update(refreshMsg{sessions: slices.Clone(pollListed), listedAt: pollAt, groups: slices.Clone(m.groups), groupPaths: m.groupPaths, groupWorktrees: m.groupWorktrees, archivedGroups: m.archivedGroups})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("stale poll reverted the painted order: %v want %v", got, want)
	}
	m.selectSessionRow(t, "alpha")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.store), []string{"b", "c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after second shift+down = %v want %v", got, want)
	}
}

func rowIDs(m *Model) []string {
	var ids []string
	for _, row := range m.rows {
		if !row.isGroup {
			ids = append(ids, row.sess.ID)
		}
	}
	return ids
}

func TestGroupReorderSurvivesInFlightPoll(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"alpha", "bravo", "charlie"} {
		if err := m.store.CreateGroup(group, dir); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "worker", dir, "alpha")
	pollSessions := slices.Clone(m.sessions)
	pollGroups := slices.Clone(m.groups)
	pollAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectGroupRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	if got, want := m.groups, []string{"bravo", "alpha", "charlie"}; !slices.Equal(got, want) {
		t.Fatalf("groups after first shift+down = %v want %v", got, want)
	}
	updated, _ = m.Update(refreshMsg{sessions: slices.Clone(pollSessions), groups: slices.Clone(pollGroups), listedAt: pollAt})
	m = updated.(*Model)
	if got, want := m.groups, []string{"bravo", "alpha", "charlie"}; !slices.Equal(got, want) {
		t.Fatalf("stale poll reverted group order: %v want %v", got, want)
	}
	m.selectGroupRow(t, "alpha")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	if got, want := m.groups, []string{"bravo", "charlie", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("groups after second shift+down = %v want %v", got, want)
	}
	stored, err := m.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, group := range stored {
		names = append(names, group.Name)
	}
	if want := []string{"bravo", "charlie", "alpha"}; !slices.Equal(names, want) {
		t.Fatalf("stored group order = %v want %v", names, want)
	}
}

func TestFreshPollRetiresReorderSnapshot(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"alpha", "bravo"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	if len(m.pendingSessionOrder) != 1 {
		t.Fatal("successful reorder did not mark its order")
	}
	fresh := refreshMsg{sessions: slices.Clone(m.sessions), listedAt: time.Now().Add(time.Second), groups: slices.Clone(m.groups)}
	updated, _ = m.Update(fresh)
	m = updated.(*Model)
	if len(m.pendingSessionOrder) != 0 {
		t.Fatal("fresh store listing did not retire reorder snapshot")
	}
}

func TestReorderKnownPreservesNewRowsAndFreshValues(t *testing.T) {
	incoming := []store.Session{
		{ID: "a", Status: "idle"},
		{ID: "new", Status: "working"},
		{ID: "b", Status: "finished"},
		{ID: "c", Status: "dead"},
	}
	got := reorderKnown(incoming, []string{"c", "a", "b"}, func(sess store.Session) string { return sess.ID }, func(store.Session) bool { return true })
	if want := []string{"c", "new", "a", "b"}; !slices.Equal([]string{got[0].ID, got[1].ID, got[2].ID, got[3].ID}, want) {
		t.Fatalf("reconciled IDs = %v want %v", got, want)
	}
	if got[0].Status != "dead" || got[1].Status != "working" || got[3].Status != "finished" {
		t.Fatalf("reconciliation lost refreshed values: %v", got)
	}
}

func TestOlderPollAfterFreshPollCannotUndoReorder(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	old := slices.Clone(m.sessions)
	oldAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	fresh := refreshMsg{sessions: slices.Clone(m.sessions), listedAt: time.Now().Add(time.Second), groups: slices.Clone(m.groups)}
	updated, _ = m.Update(fresh)
	m = updated.(*Model)
	updated, _ = m.Update(refreshMsg{sessions: old, listedAt: oldAt, groups: slices.Clone(m.groups)})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"b", "a"}; !slices.Equal(got, want) {
		t.Fatalf("older poll after a fresh one reverted order: %v want %v", got, want)
	}
}

func TestOlderListingStillDeliversNotificationFocus(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	freshAt := time.Now().Add(time.Second)
	updated, _ := m.Update(refreshMsg{
		sessions:   slices.Clone(m.sessions),
		listedAt:   freshAt,
		groupBases: map[string]string{"current": "main"},
	})
	m = updated.(*Model)
	if got := m.groupBases["current"]; got != "main" {
		t.Fatalf("fresh listing lost group base: %q", got)
	}
	m.selectSessionRow(t, "alpha")

	updated, _ = m.Update(refreshMsg{
		sessions:       []store.Session{{ID: "obsolete"}},
		listedAt:       freshAt.Add(-time.Second),
		groups:         []string{"obsolete"},
		groupBases:     map[string]string{"obsolete": "old-base"},
		focusID:        "b",
		queuedMessages: map[string]int{"b": 2},
		paneLines:      map[string]string{"b": "latest pane"},
	})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("older listing replaced current rows: %v want %v", got, want)
	}
	if len(m.groups) != 0 {
		t.Fatalf("older listing replaced current groups: %v", m.groups)
	}
	if len(m.groupBases) != 1 || m.groupBases["current"] != "main" {
		t.Fatalf("older listing replaced current group bases: %v", m.groupBases)
	}
	if selected, ok := m.selected(); !ok || selected.ID != "b" {
		t.Fatalf("notification focus was lost: selected = %q, found = %t", selected.ID, ok)
	}
	if got := m.queuedMessages["b"]; got != 2 {
		t.Fatalf("queued message count was lost: %d", got)
	}
	if got := m.paneLines["b"]; got != "latest pane" {
		t.Fatalf("pane line was lost: %q", got)
	}
}

func TestStaleGroupPollKeepsAnotherManagersUnrelatedReorder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, name := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		if err := m.store.CreateGroup(name, dir); err != nil {
			t.Fatal(err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	oldAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectGroupRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	if err := m.store.SwapGroupOrder("delta", "echo"); err != nil {
		t.Fatal(err)
	}
	stored, err := m.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	var latest []string
	for _, group := range stored {
		latest = append(latest, group.Name)
	}
	want := []string{"bravo", "alpha", "charlie", "echo", "delta"}
	if !slices.Equal(latest, want) {
		t.Fatalf("store order = %v want %v", latest, want)
	}
	updated, _ = m.Update(refreshMsg{groups: latest, listedAt: oldAt})
	m = updated.(*Model)
	if !slices.Equal(m.groups, want) {
		t.Fatalf("stale poll undid another manager's unrelated reorder: %v want %v", m.groups, want)
	}
}

func TestOlderListingStillCompletesAfterTurnRequest(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)
	if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	freshAt := time.Now().Add(time.Second)
	updated, _ := m.Update(refreshMsg{sessions: slices.Clone(m.sessions), listedAt: freshAt})
	m = updated.(*Model)
	updated, _ = m.Update(refreshMsg{
		sessions:   []store.Session{{ID: "obsolete"}},
		listedAt:   freshAt.Add(-time.Second),
		turnsEnded: []string{sess.ID},
	})
	m = updated.(*Model)
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.tmux.Exists(sess.ID) || got.Status != status.Dead || got.AfterTurn != "" {
		t.Fatalf("older listing lost the completed turn: running=%v status=%q pending=%q", m.tmux.Exists(sess.ID), got.Status, got.AfterTurn)
	}
	if rows := m.sessionRows(); len(rows) != 1 || rows[0].ID != sess.ID || rows[0].Status != status.Dead {
		t.Fatalf("older listing replaced the killed row: %+v", rows)
	}
}
