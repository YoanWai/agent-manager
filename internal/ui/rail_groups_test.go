package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"slices"
	"testing"
)

func TestArchivedViewIgnoresFold(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	m.rail.SetCollapsed("work", true)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("archived session inside a folded group should still show, got %d rows", len(m.sessionRows()))
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	active, err := m.services.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after restore, active sessions in store = %d want 1", len(active))
	}
}

func TestCollapsedStatePersistsAcrossReload(t *testing.T) {
	m := buildModel(t)
	m.rail.SetCollapsed("backend", true)
	m.rail.SetCollapsed("backend/api", true)
	if err := m.persistCollapsed(m.rail.Collapsed()); err != nil {
		t.Fatalf("persist collapsed: %v", err)
	}

	restored := loadCollapsed(m.services.store)
	if !slices.Contains(restored, "backend") || !slices.Contains(restored, "backend/api") {
		t.Fatalf("collapsed groups not restored: %v", restored)
	}

	m.rail.SetCollapsed("backend", false)
	if err := m.persistCollapsed(m.rail.Collapsed()); err != nil {
		t.Fatalf("persist expanded state: %v", err)
	}
	restored = loadCollapsed(m.services.store)
	if slices.Contains(restored, "backend") {
		t.Fatalf("expanded group leaked back as collapsed: %v", restored)
	}
	if !slices.Contains(restored, "backend/api") {
		t.Fatalf("still-folded group dropped: %v", restored)
	}
}

func TestToggleCollapseAllFlipsEveryGroup(t *testing.T) {
	m := buildModel(t)
	m.workspace.sessions = []store.Session{{ID: "a", Group: "backend/api"}, {ID: "b", Group: "frontend"}}
	m.rebuildRows()
	want := []string{"backend", "backend/api", "frontend"}

	updated, _ := m.handleKey(runeKey("F"))
	m = updated.(*Model)
	for _, group := range want {
		if !m.rail.IsCollapsed(group) {
			t.Fatalf("group %q not collapsed after fold-all", group)
		}
	}
	if restored := loadCollapsed(m.services.store); len(restored) != 3 {
		t.Fatalf("fold-all not persisted: %v", restored)
	}

	updated, _ = m.handleKey(runeKey("F"))
	m = updated.(*Model)
	for _, group := range want {
		if m.rail.IsCollapsed(group) {
			t.Fatalf("group %q still collapsed after unfold-all", group)
		}
	}
	if restored := loadCollapsed(m.services.store); len(restored) != 0 {
		t.Fatalf("unfold-all not persisted: %v", restored)
	}
}
