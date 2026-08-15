package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRebuildRowsNestsChildrenUnderParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	child := store.Session{
		ID: "sh1", Name: "term-one", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}
	if err := m.store.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	var names []string
	var depths []int
	for _, row := range m.rows {
		if row.isGroup {
			continue
		}
		names = append(names, row.sess.Name)
		depths = append(depths, row.depth)
	}
	if len(names) < 2 || names[0] != "coder" || names[1] != "term-one" {
		t.Fatalf("order = %v", names)
	}
	if depths[1] != depths[0]+1 {
		t.Fatalf("depths = %v", depths)
	}
}

func TestSearchMatchingChildKeepsParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-prod", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.search = "ssh-prod"
	m.rebuildRows()
	names := []string{}
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "coder") || !strings.Contains(strings.Join(names, ","), "ssh-prod") {
		t.Fatalf("search dropped parent: %v", names)
	}
}

func TestOrphanParentIDPaintsUnnested(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "loose", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: "gone", Status: status.Idle,
	}); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for _, row := range m.rows {
		if !row.isGroup && row.sess.Name == "loose" && row.depth == 1 {
			return
		}
	}
	t.Fatalf("orphan should sit un-nested in backend: %+v", m.rows)
}

func TestArchiveViewShowsNestedShellWhenParentLive(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "old-term", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := m.store.SetArchived("sh1", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	var names []string
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "old-term") {
		t.Fatalf("archived nested shell missing: %v", names)
	}
	if strings.Contains(joined, "coder") {
		t.Fatalf("live parent leaked into archive view: %v", names)
	}
}

func TestStatusFilterHoldsNestedIdleShell(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "term-hold", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "term-hold")
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if !strings.Contains(strings.Join(sessionNames(m), ","), "term-hold") {
		t.Fatalf("held nested shell dropped: %v", sessionNames(m))
	}
	sess, ok := m.selected()
	if !ok || sess.Name != "term-hold" {
		t.Fatalf("cursor left the held shell: %+v %v", sess, ok)
	}
}

func TestSearchMatchingArchivedChildDoesNotHoistLiveParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-old", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := m.store.SetArchived("sh1", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	m.search = "ssh-old"
	m.rebuildRows()
	var names []string
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "ssh-old") {
		t.Fatalf("archived child missing: %v", names)
	}
	if strings.Contains(joined, "coder") {
		t.Fatalf("search hoisted live parent into archive view: %v", names)
	}
}

func TestSettingsHasNoTerminalRows(t *testing.T) {
	m := buildModel(t)
	if strings.Contains(ansi.Strip(m.viewSettings()), "terminal rows") {
		t.Fatal("terminal rows setting must be gone")
	}
}

func TestReorderAgentSkipsChildren(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	_, cmd := m.reorderSelected(1)
	m.applyCmd(t, cmd)
	var names []string
	for _, row := range m.rows {
		if !row.isGroup && row.sess.ParentID == "" {
			names = append(names, row.sess.Name)
		}
	}
	if len(names) < 2 || names[0] != "other" || names[1] != "coder" {
		t.Fatalf("un-nested order %v", names)
	}
}
