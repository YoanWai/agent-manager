package ui

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	sessiondialogs "github.com/YoanWai/agent-manager/internal/ui/session/dialogs"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNestedGroupsTree(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestToggleEmptyGroupsFiltersTreeWithoutDeletingGroups(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width = 200
	for _, group := range []string{"empty", "work", "work/leaf", "work/unused"} {
		if err := m.services.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "nested", t.TempDir(), "work/leaf")

	m.selectGroupRow(t, "empty")
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m.drainEffects(t)
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
	m.drainEffects(t)
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
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
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
	t.Parallel()
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
		m.drainEffects(t)
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
	t.Parallel()
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
	m.drainEffects(t)
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
	m.layout.width, m.layout.height = 120, 34
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
	m.drainEffects(t)
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
	t.Parallel()
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
	rail := ansi.Strip(strings.Join(uitheme.SplitLines(joinContentText(m.railLines(40, 20))), "\n"))
	if !strings.Contains(rail, "nothing needs attention") {
		t.Fatalf("rail missing empty attention copy:\n%s", rail)
	}
}

func TestStatusFilterGroupRosterMatchesCount(t *testing.T) {
	t.Parallel()
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

	if got := uirail.GroupSessionCount(m.railAgents(), "fleet"); got != 1 {
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
	// Confirmation reads the last poll's pane projection; the worker will
	// revalidate the real windows before acting.
	m.workspace.panes = map[string]tmux.Pane{"w": {PID: 1}, "busy": {PID: 2}}
	m.rail.SetFilteringAttention(true)
	m.rebuildRows()

	updated, _ := m.killAllLive()
	m = updated.(*Model)
	if len(pendingConfirm(m).sessions) != 1 || pendingConfirm(m).sessions[0].ID != "w" {
		t.Fatalf("filtered kill-all targets = %+v, want only the listed session", pendingConfirm(m).sessions)
	}
	m.base = baseList
	m.clearSurfaces()

	if got := sessiondialogs.InGroup(m.listedSessions(), ""); len(got) != 1 || got[0].ID != "w" {
		t.Fatalf("filtered group sessions = %+v, want only the listed session", got)
	}

	updated, _ = m.reviveAllDead()
	m.drainEffects(t)
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
	m.drainEffects(t)
	m = updated.(*Model)
	if confirmOpen(m) {
		t.Fatalf("hidden sessions were counted: %q", pendingConfirm(m).label)
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
	t.Parallel()
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
	if runtime.GOOS == "windows" {
		tool.ForkCommand = testfixture.ModeCommand("cat-stdin") + " {id}"
	}
	m.services.cfg.Tools[source.Tool] = tool

	m.openFork()
	openFork(m).SetName("fork-under-filter")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if m.rail.FilteringAttention() {
		t.Fatalf("fork should clear the filter, got %v", m.rail.FilteringAttention())
	}
	if entry, ok := m.selectedRow(); !ok || entry.isGroup || entry.sess.Name != "fork-under-filter" {
		t.Fatalf("cursor should land on the forked row, got %+v", entry)
	}
}

func menuEntry(t *testing.T, m *Model, label string) int {
	t.Helper()
	preparedView(m)
	for line := 1; line+1 < len(m.layout.displayedRail.Menu); line++ {
		if strings.Contains(ansi.Strip(m.layout.displayedRail.Menu[line]), label) {
			return line - 1
		}
	}
	t.Fatalf("the menu has no %q entry", label)
	return -1
}

func TestRootMenuOpensSettings(t *testing.T) {
	m := buildModel(t)
	m.rebuildRows()
	preparedView(m)
	y0, _ := m.bodyYRange()
	m.handleMouse(tea.MouseMsg{X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	m.View()
	settings := menuEntry(t, m, "Settings")
	if line := strings.Fields(ansi.Strip(m.layout.displayedRail.Menu[settings+1])); len(line) < 3 || line[len(line)-2] != "s" {
		t.Fatalf("the Settings entry should name its key: %q", m.layout.displayedRail.Menu[settings+1])
	}
	_, cmd := leftClickCmd(m, m.layout.displayedRail.MenuRect.Left+2, m.layout.displayedRail.MenuRect.Top+1+settings)
	m.runBatch(t, cmd)
	if m.rail.MenuOpen() || !m.settingsOpen() {
		t.Fatal("root menu must open Settings even in an empty workspace")
	}
}

// An entry runs on the release over the entry it was pressed on, so an
// attach or an editor it starts never receives the button coming back up.
func TestAMenuEntryRunsOnTheRelease(t *testing.T) {
	m := buildModel(t)
	m.rebuildRows()
	preparedView(m)
	y0, _ := m.bodyYRange()
	m.handleMouse(tea.MouseMsg{X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	m.View()
	settings, other := menuEntry(t, m, "Settings"), menuEntry(t, m, "New session")
	menu := m.layout.displayedRail.MenuRect
	x, y := menu.Left+2, menu.Top+1+settings
	if m = leftPress(m, x, y); !m.rail.MenuOpen() || !m.onList() {
		t.Fatalf("the press alone ran the entry, menu %v mode %v", m.rail.MenuOpen(), m.screenID())
	}
	if m = leftRelease(m, x, menu.Top+1+other); !m.rail.MenuOpen() || !m.onList() {
		t.Fatalf("a press slid onto another entry ran it, menu %v mode %v", m.rail.MenuOpen(), m.screenID())
	}
	m = leftPress(m, x, y)
	m, cmd := leftReleaseCmd(m, x, y)
	m.runBatch(t, cmd)
	if m.rail.MenuOpen() || !m.settingsOpen() {
		t.Fatalf("the release should run the entry, menu %v mode %v", m.rail.MenuOpen(), m.screenID())
	}
}

func TestMenuStaysOpenAfterTheDotsRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	frame := strings.Split(ansi.Strip(preparedView(m)), "\n")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	if !strings.Contains(frame[y], "[…]") {
		t.Fatalf("the selected row should paint its menu button: %q", frame[y])
	}
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.rail.MenuOpen() {
		t.Fatal("a click on […] should open the row menu")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.rail.MenuOpen() || !m.onList() {
		t.Fatalf("the release after […] should leave the menu up and unfocused, menu = %v screen = %v", m.rail.MenuOpen(), m.screenID())
	}
}

func TestRightClickOpensTheRowMenuAndRunsAnEntry(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	m = railMouse(t, m, "beta", tea.MouseActionPress, tea.MouseButtonRight)
	if !m.rail.MenuOpen() {
		t.Fatal("right click should open the row menu")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("right click should select its row, got %q", sess.Name)
	}
	frame := ansi.Strip(preparedView(m))
	for _, label := range []string{"Attach", "Rename", "Delete"} {
		if !strings.Contains(frame, label) {
			t.Fatalf("menu should list %s:\n%s", label, frame)
		}
	}
	rename := menuEntry(t, m, "Rename")
	m, _ = leftClickCmd(m, m.layout.displayedRail.MenuRect.Left+2, m.layout.displayedRail.MenuRect.Top+1+rename)
	if m.rail.MenuOpen() || !renameOpen(m) {
		t.Fatalf("clicking Rename should open rename, menu = %v screen = %v", m.rail.MenuOpen(), m.screenID())
	}
}

func TestRowMenuClosesOnEscAndOutsideClick(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")

	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.MenuOpen() {
		t.Fatal("esc should close the menu")
	}
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	preparedView(m)
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.width - 1, Y: m.layout.height - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if m.rail.MenuOpen() || !m.onList() {
		t.Fatalf("a click outside should close the menu only, menu = %v screen = %v", m.rail.MenuOpen(), m.screenID())
	}
}

func TestRightClickOnTheRailWhileFocusedOpensTheMenu(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)

	m = railMouse(t, m, "beta", tea.MouseActionPress, tea.MouseButtonRight)
	if !m.onList() || !m.rail.MenuOpen() {
		t.Fatalf("right click on the rail should leave focus and open the menu, screen = %v menu = %v", m.screenID(), m.rail.MenuOpen())
	}
}

func TestGroupMenuCreatesAndNeverAttaches(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")
	m.selectGroupRow(t, "work")
	m = railMouseAtLine(t, m, paintedGroupLines(t, m, "work")[0], tea.MouseActionPress, tea.MouseButtonRight)
	preparedView(m)
	joined := ansi.Strip(strings.Join(m.layout.displayedRail.Menu, "\n"))
	if !strings.Contains(joined, "New session") {
		t.Fatalf("group menu should offer creation:\n%s", joined)
	}
	if !strings.Contains(joined, "Quick prompt mode") || strings.Contains(joined, "Attach") {
		t.Fatalf("group menu should create and never attach:\n%s", joined)
	}
	quick := menuEntry(t, m, "Quick prompt mode")
	m, _ = leftClickCmd(m, m.layout.displayedRail.MenuRect.Left+2, m.layout.displayedRail.MenuRect.Top+1+quick)
	if (m.quick() == nil) || !m.onList() {
		t.Fatalf("group menu should open quick prompt mode, active = %v screen = %v", (m.quick() != nil), m.screenID())
	}
}

func TestGroupMenuReviveAsksLikeTheKey(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work")
	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)
	m = railMouseAtLine(t, m, paintedGroupLines(t, m, "work")[0], tea.MouseActionPress, tea.MouseButtonRight)
	revive := menuEntry(t, m, "Revive")
	m, _ = leftClickCmd(m, m.layout.displayedRail.MenuRect.Left+2, m.layout.displayedRail.MenuRect.Top+1+revive)
	if !confirmOpen(m) || !pendingConfirm(m).isGroup {
		t.Fatalf("Revive on a group with two dead sessions should open the group card, screen = %v isGroup = %v", m.screenID(), pendingConfirm(m).isGroup)
	}
	for _, sess := range m.workspace.sessions {
		if m.services.tmux.Exists(sess.ID) {
			t.Fatalf("the menu revived %s before the question was answered", sess.Name)
		}
	}
}

func TestDotsOnAnUnselectedRowOpenItsMenuAtOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "beta")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if sess, ok := m.selected(); !m.rail.MenuOpen() || !ok || sess.Name != "beta" {
		t.Fatalf("[…] on beta should open beta's menu in one click, menu = %v selected = %q", m.rail.MenuOpen(), sess.Name)
	}
}

func TestMenuPicksTheEntryAPressIsReleasedOn(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	preparedView(m)
	rename := menuEntry(t, m, "Rename")
	at := tea.MouseMsg{X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + rename, Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionMotion
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if !m.rail.MenuOpen() {
		t.Fatal("dragging over Rename closed the menu")
	}
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if !renameOpen(m) {
		t.Fatalf("releasing on Rename should run it, screen = %v", m.screenID())
	}
}

func TestMenuHighlightsTheEntryUnderAHover(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	y0, _ := m.bodyYRange()
	y := y0 + paintedRailLines(t, m, "alpha")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if cmd := m.syncMouseCapture(); cmd == nil || !m.mouse.hover {
		t.Fatal("an open menu should ask the terminal for every motion")
	}
	preparedView(m)
	last := len(m.layout.displayedRail.Menu) - 3
	updated, _ = m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.MenuRect.Left + 2, Y: m.layout.displayedRail.MenuRect.Top + 1 + last, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = updated.(*Model)
	if !m.rail.MenuOpen() {
		t.Fatal("hovering the last entry closed the menu")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd := m.syncMouseCapture(); cmd == nil || m.mouse.hover {
		t.Fatal("closing the menu should hand motion back to button tracking")
	}
}

func railMouseAtLine(t *testing.T, m *Model, line int, action tea.MouseAction, button tea.MouseButton) *Model {
	t.Helper()
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + line, Action: action, Button: button})
	return updated.(*Model)
}

// The pending end is reachable both ways: the menu entry for the mouse,
// the footer naming its key for the keyboard.
func TestAPendingEndOffersItsCancelInTheMenuAndTheFooter(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	askAfterTurn(t, m, "alpha", store.AfterTurnArchive)
	loadStoredRows(t, m)
	m.selectSessionRow(t, "alpha")

	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"alpha ↓", "archives when this turn ends", "c cancel archive"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the frame does not show %q:\n%s", want, frame)
		}
	}
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	cancel := menuEntry(t, m, "Cancel archive")
	m, _ = leftClickCmd(m, m.layout.displayedRail.MenuRect.Left+2, m.layout.displayedRail.MenuRect.Top+1+cancel)
	m.drainEffects(t)
	sess, _ := m.selected()
	if got, _ := m.services.store.Get(sess.ID); got.AfterTurn != "" {
		t.Fatalf("Cancel archive left %q pending", got.AfterTurn)
	}
	if strings.Contains(ansi.Strip(preparedView(m)), "alpha ↓") {
		t.Fatal("the row still wears the archive mark after the cancel")
	}
}

// A row the poll has not yet ended still carries its request once dead, and
// c cancels it there, so the menu has to as well.
func TestADeadRowWithAPendingEndOffersItsCancel(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)
	if err := m.services.store.UpdateStatus(sess.ID, status.Dead); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	loadStoredRows(t, m)
	m = railMouse(t, m, "alpha", tea.MouseActionPress, tea.MouseButtonRight)
	cancel := menuEntry(t, m, "Cancel kill")
	m, _ = leftClickCmd(m, m.layout.displayedRail.MenuRect.Left+2, m.layout.displayedRail.MenuRect.Top+1+cancel)
	m.drainEffects(t)
	if got, _ := m.services.store.Get(sess.ID); got.AfterTurn != "" {
		t.Fatalf("Cancel kill on the dead row left %q pending", got.AfterTurn)
	}
}
