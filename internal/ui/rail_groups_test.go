package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
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

	m.rail.collapsed["work"] = true

	m.rail.showArchived = true
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
	m.rail.collapsed["backend"] = true
	m.rail.collapsed["backend/api"] = true
	m.persistCollapsed()

	restored := loadCollapsed(m.services.store)
	if !restored["backend"] || !restored["backend/api"] {
		t.Fatalf("collapsed groups not restored: %v", restored)
	}

	m.rail.collapsed["backend"] = false
	m.persistCollapsed()
	restored = loadCollapsed(m.services.store)
	if restored["backend"] {
		t.Fatalf("expanded group leaked back as collapsed: %v", restored)
	}
	if !restored["backend/api"] {
		t.Fatalf("still-folded group dropped: %v", restored)
	}
}

func TestToggleCollapseAllFlipsEveryGroup(t *testing.T) {
	m := buildModel(t)
	m.workspace.sessions = []store.Session{{ID: "a", Group: "backend/api"}, {ID: "b", Group: "frontend"}}
	want := []string{"backend", "backend/api", "frontend"}

	m.toggleCollapseAll()
	for _, group := range want {
		if !m.rail.collapsed[group] {
			t.Fatalf("group %q not collapsed after fold-all", group)
		}
	}
	if restored := loadCollapsed(m.services.store); len(restored) != 3 {
		t.Fatalf("fold-all not persisted: %v", restored)
	}

	m.toggleCollapseAll()
	for _, group := range want {
		if m.rail.collapsed[group] {
			t.Fatalf("group %q still collapsed after unfold-all", group)
		}
	}
	if restored := loadCollapsed(m.services.store); len(restored) != 0 {
		t.Fatalf("unfold-all not persisted: %v", restored)
	}
}
