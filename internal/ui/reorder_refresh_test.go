package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// A Rail reorder completes on the effect lane, and its completion fences
// every listing read before it: an in-flight poll cannot paint the old order.
func TestReorderSurvivesInFlightPoll(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	pollListed := slices.Clone(m.workspace.sessions)
	pollAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := listSessionIDs(t, m.services.store), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after first shift+down = %v want %v", got, want)
	}
	updated, _ = m.Update(refreshMsg{sessions: slices.Clone(pollListed), listedAt: pollAt, groups: slices.Clone(m.workspace.groups),
		groupPaths: m.workspace.groupPaths, groupWorktrees: m.workspace.groupWorktrees, archivedGroups: m.workspace.archivedGroups})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("stale poll reverted the painted order: %v want %v", got, want)
	}
	m.selectSessionRow(t, "alpha")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := listSessionIDs(t, m.services.store), []string{"b", "c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after second shift+down = %v want %v", got, want)
	}
}

func rowIDs(m *Model) []string {
	var ids []string
	for _, sess := range m.sessionRows() {
		ids = append(ids, sess.ID)
	}
	return ids
}

func TestGroupReorderSurvivesInFlightPoll(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"alpha", "bravo", "charlie"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "worker", dir, "alpha")
	pollSessions := slices.Clone(m.workspace.sessions)
	pollGroups := slices.Clone(m.workspace.groups)
	pollAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectGroupRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := m.workspace.groups, []string{"bravo", "alpha", "charlie"}; !slices.Equal(got, want) {
		t.Fatalf("groups after first shift+down = %v want %v", got, want)
	}
	updated, _ = m.Update(refreshMsg{sessions: slices.Clone(pollSessions), groups: slices.Clone(pollGroups), listedAt: pollAt})
	m = updated.(*Model)
	if got, want := m.workspace.groups, []string{"bravo", "alpha", "charlie"}; !slices.Equal(got, want) {
		t.Fatalf("stale poll reverted group order: %v want %v", got, want)
	}
	m.selectGroupRow(t, "alpha")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := m.workspace.groups, []string{"bravo", "charlie", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("groups after second shift+down = %v want %v", got, want)
	}
	stored, err := m.services.store.Groups()
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

// A listing read after the reorder completed is current and applies whole.
func TestFreshPollAfterReorderApplies(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"alpha", "bravo"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	stored, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	stored = append(stored, store.Session{ID: "late", Name: "late", Tool: "claude", Cwd: "/tmp", Status: status.Idle})
	updated, _ = m.Update(refreshMsg{sessions: stored, listedAt: time.Now().Add(time.Second), groups: slices.Clone(m.workspace.groups)})
	m = updated.(*Model)
	if got := rowIDs(m); len(got) != 3 || got[2] != "late" {
		t.Fatalf("fresh listing after the reorder was not applied: %v", got)
	}
}

func TestOlderPollAfterFreshPollCannotUndoReorder(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	old := slices.Clone(m.workspace.sessions)
	oldAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	fresh := refreshMsg{sessions: slices.Clone(m.workspace.sessions), listedAt: time.Now().Add(time.Second), groups: slices.Clone(m.workspace.groups)}
	updated, _ = m.Update(fresh)
	m = updated.(*Model)
	updated, _ = m.Update(refreshMsg{sessions: old, listedAt: oldAt, groups: slices.Clone(m.workspace.groups)})
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
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	freshAt := time.Now().Add(time.Second)
	updated, _ := m.Update(refreshMsg{
		sessions:   slices.Clone(m.workspace.sessions),
		listedAt:   freshAt,
		groupBases: map[string]string{"current": "main"},
	})
	m = updated.(*Model)
	if got := m.workspace.groupBases["current"]; got != "main" {
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
	if len(m.workspace.groups) != 0 {
		t.Fatalf("older listing replaced current groups: %v", m.workspace.groups)
	}
	if len(m.workspace.groupBases) != 1 || m.workspace.groupBases["current"] != "main" {
		t.Fatalf("older listing replaced current group bases: %v", m.workspace.groupBases)
	}
	if selected, ok := m.selected(); !ok || selected.ID != "b" {
		t.Fatalf("notification focus was lost: selected = %q, found = %t", selected.ID, ok)
	}
	if got := m.workspace.queuedMessages["b"]; got != 2 {
		t.Fatalf("queued message count was lost: %d", got)
	}
	if got := m.workspace.paneLines["b"]; got != "latest pane" {
		t.Fatalf("pane line was lost: %q", got)
	}
}

// A stale listing is discarded whole, so another manager's unrelated reorder
// it carried lands with the next current listing instead.
func TestStaleGroupPollKeepsAnotherManagersUnrelatedReorder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	for _, name := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		if err := m.services.store.CreateGroup(name, dir); err != nil {
			t.Fatal(err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	oldAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectGroupRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if err := m.services.store.SwapGroupOrder("delta", "echo"); err != nil {
		t.Fatal(err)
	}
	stored, err := m.services.store.Groups()
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
	if got := m.workspace.groups; !slices.Equal(got[:3], want[:3]) {
		t.Fatalf("stale poll undid this manager's reorder: %v", got)
	}
	m.applyCmd(t, m.refreshCmd())
	if !slices.Equal(m.workspace.groups, want) {
		t.Fatalf("the next listing lost another manager's unrelated reorder: %v want %v", m.workspace.groups, want)
	}
}

func TestOlderListingStillCompletesAfterTurnRequest(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)
	if err := m.services.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	freshAt := time.Now().Add(time.Second)
	updated, _ := m.Update(refreshMsg{sessions: slices.Clone(m.workspace.sessions), listedAt: freshAt})
	m = updated.(*Model)
	updated, _ = m.Update(refreshMsg{
		sessions:   []store.Session{{ID: "obsolete"}},
		listedAt:   freshAt.Add(-time.Second),
		turnsEnded: []string{sess.ID},
	})
	m = updated.(*Model)
	m.drainEffects(t)
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.services.tmux.Exists(sess.ID) || got.Status != status.Dead || got.AfterTurn != "" {
		t.Fatalf("older listing lost the completed turn: running=%v status=%q pending=%q", m.services.tmux.Exists(sess.ID), got.Status, got.AfterTurn)
	}
	if rows := m.sessionRows(); len(rows) != 1 || rows[0].ID != sess.ID || rows[0].Status != status.Dead {
		t.Fatalf("older listing replaced the killed row: %+v", rows)
	}
}
