package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"strconv"
	"strings"
	"testing"
)

func TestNestedGroupsTree(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("backend/api/auth", ""); err != nil {
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

	if !railRows(m)[0].isRoot() {
		t.Fatalf("root row should lead the list, rows[0] = %+v", railRows(m)[0])
	}
	if railRows(m)[1].isGroup || railRows(m)[1].sess.Name != "top" {
		t.Fatalf("the top-level session should follow root, rows[1] = %+v", railRows(m)[1])
	}

	deep := m.sessionRows()[1]
	if deep.Group != "backend/api/auth" {
		t.Fatalf("deep session group = %q", deep.Group)
	}

	m.rail.SetCollapsed("backend", true)
	m.rebuildRows()
	if len(m.sessionRows()) != 1 {
		t.Fatalf("collapsing backend should hide the deep session, got %d sessions", len(m.sessionRows()))
	}
	m.rail.SetCollapsed("backend", false)
	m.rebuildRows()

	m.rail.SetSearch("deep", m.rail.Searching())
	m.rebuildRows()
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Name != "deep" {
		t.Fatalf("search should keep only deep, got %v", sessions)
	}
	m.rail.SetSearch("", m.rail.Searching())
	m.rebuildRows()

	if preparedView(m) == "" {
		t.Fatal("View should render non-empty")
	}
}

func TestRebuildRowsNestsChildrenUnderParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	child := store.Session{
		ID: "sh1", Name: "term-one", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}
	if err := m.services.store.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	var names []string
	var depths []int
	for _, row := range railRows(m) {
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
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.services.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-prod", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("ssh-prod", m.rail.Searching())
	m.rebuildRows()
	names := []string{}
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "coder") || !strings.Contains(strings.Join(names, ","), "ssh-prod") {
		t.Fatalf("search dropped parent: %v", names)
	}
}

func TestSearchCarriedParentKeepsStoreOrder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "carrier", dir, "backend")
	carrier := m.sessionRows()[0]
	if err := m.services.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-prod", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: carrier.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	createSession(t, m, "ssh-runner", dir, "backend")
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("ssh", m.rail.Searching())
	m.rebuildRows()
	names := []string{}
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	want := []string{"carrier", "ssh-prod", "ssh-runner"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestOrphanParentIDPaintsUnnested(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.CreateSession(store.Session{
		ID: "gone", Name: "parent", Tool: "claude", Cwd: dir,
		Group: "backend", Status: status.Idle,
	}); err != nil {
		t.Fatalf("parent: %v", err)
	}
	if err := m.services.store.CreateSession(store.Session{
		ID: "sh1", Name: "loose", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: "gone", Status: status.Idle,
	}); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	if err := m.services.store.Delete("gone"); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.Name == "loose" && row.depth == 1 {
			return
		}
	}
	t.Fatalf("orphan should sit un-nested in backend: %+v", railRows(m))
}

func TestArchiveViewShowsNestedShellWhenParentLive(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.services.store.CreateSession(store.Session{
		ID: "sh1", Name: "old-term", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := m.services.store.SetArchived("sh1", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	var names []string
	for _, row := range railRows(m) {
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
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.services.store.CreateSession(store.Session{
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

func TestUnlistedParentsLeaveChildrenInStoreOrder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "first", dir, "backend")
	createSession(t, m, "second", dir, "backend")
	rows := m.sessionRows()
	for i, parent := range rows {
		child := store.Session{
			ID: "sh" + strconv.Itoa(i), Name: "term-" + strconv.Itoa(i), Tool: "terminal",
			Cwd: dir, Group: "backend", ParentID: parent.ID, Status: status.Idle,
		}
		if err := m.services.store.CreateSession(child); err != nil {
			t.Fatalf("child %d: %v", i, err)
		}
	}
	for _, parent := range rows {
		if err := m.services.store.SetArchived(parent.ID, true); err != nil {
			t.Fatalf("archive %s: %v", parent.ID, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
			if row.depth != 1 {
				t.Fatalf("%s painted nested: depth %d", row.sess.Name, row.depth)
			}
		}
	}
	want := []string{"term-0", "term-1"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestSearchMatchingArchivedChildDoesNotHoistLiveParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.services.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-old", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := m.services.store.SetArchived("sh1", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("ssh-old", m.rail.Searching())
	m.rebuildRows()
	var names []string
	for _, row := range railRows(m) {
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
