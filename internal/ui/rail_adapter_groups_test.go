package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestArchivedViewIgnoresFold(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetCollapsed("work", true)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("archived session inside a folded group should still show, got %d rows", len(m.sessionRows()))
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	active, err := m.services.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after restore, active sessions in store = %d want 1", len(active))
	}
}

func (m *Model) persistCollapsed(paths []string) error {
	_, err := m.bindEffect(railRequest{mutation: uirail.Mutation{Kind: uirail.SaveCollapsed, Collapsed: append([]string(nil), paths...)}})()
	return err
}

func TestCollapsedStatePersistsAcrossReload(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.rail.SetCollapsed("backend", true)
	m.rail.SetCollapsed("backend/api", true)
	if err := m.persistCollapsed(m.rail.Collapsed()); err != nil {
		t.Fatalf("persist collapsed: %v", err)
	}
	if restored := uirailpersist.Load(m.services.store); !restored.IsCollapsed("backend/api") {
		t.Fatal("a reload lost the saved folds")
	}

	m.rail.SetCollapsed("backend", false)
	if err := m.persistCollapsed(m.rail.Collapsed()); err != nil {
		t.Fatalf("persist expanded state: %v", err)
	}
	restored := uirailpersist.Load(m.services.store)
	if restored.IsCollapsed("backend") {
		t.Fatal("expanded group leaked back as collapsed")
	}
	if !restored.IsCollapsed("backend/api") {
		t.Fatal("still-folded group dropped")
	}
}

func TestToggleCollapseAllFlipsEveryGroup(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.workspace.sessions = []store.Session{{ID: "a", Group: "backend/api"}, {ID: "b", Group: "frontend"}}
	m.rebuildRows()
	want := []string{"backend", "backend/api", "frontend"}

	updated, _ := m.handleKey(runeKey("F"))
	m.drainEffects(t)
	m = updated.(*Model)
	for _, group := range want {
		if !m.rail.IsCollapsed(group) {
			t.Fatalf("group %q not collapsed after fold-all", group)
		}
	}
	if restored := uirailpersist.Collapsed(m.services.store); len(restored) != 3 {
		t.Fatalf("fold-all not persisted: %v", restored)
	}

	updated, _ = m.handleKey(runeKey("F"))
	m.drainEffects(t)
	m = updated.(*Model)
	for _, group := range want {
		if m.rail.IsCollapsed(group) {
			t.Fatalf("group %q still collapsed after unfold-all", group)
		}
	}
	if restored := uirailpersist.Collapsed(m.services.store); len(restored) != 0 {
		t.Fatalf("unfold-all not persisted: %v", restored)
	}
}
