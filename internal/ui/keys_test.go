package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNestedGroupsTree(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("backend/api/auth", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	createSession(t, m, "deep", dir, "backend/api/auth")
	createSession(t, m, "top", dir, "")

	groupPaths := m.groupRowPaths()
	want := []string{"backend", "backend/api", "backend/api/auth"}
	if len(groupPaths) != len(want) {
		t.Fatalf("group rows = %v want %v", groupPaths, want)
	}
	for i := range want {
		if groupPaths[i] != want[i] {
			t.Fatalf("group rows = %v want %v", groupPaths, want)
		}
	}

	if !m.rows[0].isRoot() {
		t.Fatalf("root row should lead the list, rows[0] = %+v", m.rows[0])
	}
	if m.rows[1].isGroup || m.rows[1].sess.Name != "top" {
		t.Fatalf("the top-level session should follow root, rows[1] = %+v", m.rows[1])
	}

	deep := m.sessionRows()[1]
	if deep.Group != "backend/api/auth" {
		t.Fatalf("deep session group = %q", deep.Group)
	}

	m.collapsed["backend"] = true
	m.rebuildRows()
	if len(m.sessionRows()) != 1 {
		t.Fatalf("collapsing backend should hide the deep session, got %d sessions", len(m.sessionRows()))
	}
	m.collapsed["backend"] = false
	m.rebuildRows()

	m.search = "deep"
	m.rebuildRows()
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Name != "deep" {
		t.Fatalf("search should keep only deep, got %v", sessions)
	}
	m.search = ""
	m.rebuildRows()

	if m.View() == "" {
		t.Fatal("View should render non-empty")
	}
}

func TestPortableReorderKeysSwapVisibleSessions(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "keep-charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.search = "keep"
	loadStoredRows(t, m)
	m.selectSessionRow(t, "keep-charlie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	if got, want := []string{m.sessionRows()[0].ID, m.sessionRows()[1].ID}, []string{"c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("visible order after K = %v want %v", got, want)
	}
	if got, want := listSessionIDs(t, m.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after K = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.store), []string{"a", "hidden", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after J = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after shift+up = %v want %v", got, want)
	}
}

func TestReorderGroupSkipsFilteredSibling(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"alpha", "hidden", "gamma"} {
		if err := m.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Group: "alpha", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Group: "hidden", Status: "idle"},
		{ID: "g", Name: "keep-gamma", Tool: "claude", Cwd: "/tmp", Group: "gamma", Status: "idle"},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.search = "keep"
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	if got, want := m.groupRowPaths(), []string{"gamma", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("visible group order after K = %v want %v", got, want)
	}
	groups, err := m.store.Groups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	got := make([]string, len(groups))
	for i, group := range groups {
		got[i] = group.Name
	}
	if want := []string{"gamma", "hidden", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("stored group order after K = %v want %v", got, want)
	}
}

func TestReorderSyntheticGroupUpdatesImmediately(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"alpha/deep", "beta/deep", "gamma/deep"} {
		if err := m.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	var roots []string
	for _, group := range m.groupRowPaths() {
		if !strings.Contains(group, "/") {
			roots = append(roots, group)
		}
	}
	if want := []string{"alpha", "gamma", "beta"}; !slices.Equal(roots, want) {
		t.Fatalf("root order after K = %v want %v", roots, want)
	}
}

func TestToggleEmptyGroupsFiltersTreeWithoutDeletingGroups(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"empty", "work", "work/leaf", "work/unused"} {
		if err := m.store.CreateGroup(group, ""); err != nil {
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
	groups, err := m.store.Groups()
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

func TestArchivedViewIgnoresFold(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.collapsed["work"] = true

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("archived session inside a folded group should still show, got %d rows", len(m.sessionRows()))
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	active, err := m.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after restore, active sessions in store = %d want 1", len(active))
	}
}

func TestCursorWrapsAroundTheList(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "first", dir, "")
	createSession(t, m, "second", dir, "")

	m.cursor = 0
	m.moveCursor(-1)
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("up from the top should wrap to the bottom, cursor = %d", m.cursor)
	}
	m.moveCursor(1)
	if m.cursor != 0 {
		t.Fatalf("down from the bottom should wrap to the top, cursor = %d", m.cursor)
	}

	m.rows = nil
	m.cursor = 0
	m.moveCursor(1)
	if m.cursor != 0 {
		t.Fatalf("empty list should leave the cursor alone, cursor = %d", m.cursor)
	}
}

func TestCollapsedStatePersistsAcrossReload(t *testing.T) {
	m := buildModel(t)
	m.collapsed["backend"] = true
	m.collapsed["backend/api"] = true
	m.persistCollapsed()

	restored := loadCollapsed(m.store)
	if !restored["backend"] || !restored["backend/api"] {
		t.Fatalf("collapsed groups not restored: %v", restored)
	}

	m.collapsed["backend"] = false
	m.persistCollapsed()
	restored = loadCollapsed(m.store)
	if restored["backend"] {
		t.Fatalf("expanded group leaked back as collapsed: %v", restored)
	}
	if !restored["backend/api"] {
		t.Fatalf("still-folded group dropped: %v", restored)
	}
}

func TestToggleCollapseAllFlipsEveryGroup(t *testing.T) {
	m := buildModel(t)
	m.sessions = []store.Session{{ID: "a", Group: "backend/api"}, {ID: "b", Group: "frontend"}}
	want := []string{"backend", "backend/api", "frontend"}

	m.toggleCollapseAll()
	for _, group := range want {
		if !m.collapsed[group] {
			t.Fatalf("group %q not collapsed after fold-all", group)
		}
	}
	if restored := loadCollapsed(m.store); len(restored) != 3 {
		t.Fatalf("fold-all not persisted: %v", restored)
	}

	m.toggleCollapseAll()
	for _, group := range want {
		if m.collapsed[group] {
			t.Fatalf("group %q still collapsed after unfold-all", group)
		}
	}
	if restored := loadCollapsed(m.store); len(restored) != 0 {
		t.Fatalf("unfold-all not persisted: %v", restored)
	}
}
