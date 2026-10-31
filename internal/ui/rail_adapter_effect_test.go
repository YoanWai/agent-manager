package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCursorWrapsAroundTheList(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "first", dir, "")
	createSession(t, m, "second", dir, "")

	setRailCursor(m, 0)
	m.moveCursor(-1)
	if m.rail.Cursor() != len(railRows(m))-1 {
		t.Fatalf("up from the top should wrap to the bottom, cursor = %d", m.rail.Cursor())
	}
	m.moveCursor(1)
	if m.rail.Cursor() != 0 {
		t.Fatalf("down from the bottom should wrap to the top, cursor = %d", m.rail.Cursor())
	}

	resetRailModel(m)
	m.moveCursor(1)
	if m.rail.Cursor() != 0 {
		t.Fatalf("empty list should leave the cursor alone, cursor = %d", m.rail.Cursor())
	}
}

// Right steps into the row under the cursor: a session is focused, and a
// collapsed group opens without the toggle closing an open one.
func TestRightStepsIntoTheRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("grouped", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "stepin", t.TempDir(), "grouped")
	m.selectGroupRow(t, "grouped")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if !m.rail.IsCollapsed("grouped") {
		t.Fatal("left did not close the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.rail.IsCollapsed("grouped") {
		t.Fatal("right did not open the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.rail.IsCollapsed("grouped") {
		t.Fatal("a second right closed the group it had opened")
	}

	m.selectSessionRow(t, "stepin")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("right did not focus the session, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
}

func TestNamesContainingTheHandleStillSelectAndDrag(t *testing.T) {
	m := buildModel(t)
	name := "界e\u0301 " + m.glyphs().ReorderGrip + " notes"
	createSession(t, m, name, t.TempDir(), "")
	createSession(t, m, "other", t.TempDir(), "")
	m.View()
	line := paintedRailLines(t, m, name)[0]
	y0, _ := m.bodyYRange()
	frame := strings.Split(ansi.Strip(m.View()), "\n")
	head := frame[y0+line]
	nameAt := strings.Index(head, name)
	if nameAt < 0 {
		t.Fatalf("row lost its Unicode name: %q", head)
	}
	x := ansi.StringWidth(head[:nameAt]) + ansi.StringWidth("界e\u0301 ")
	m.handleMouse(tea.MouseMsg{X: x, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.rail.Reordering() {
		t.Fatal("clicking punctuation in the name lifted the row")
	}
	m.handleMouse(tea.MouseMsg{X: x, Y: y0 + line, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = liftByHandle(t, m, name)
	if !m.rail.Reordering() || railSelectedSession(m).Name != name {
		t.Fatal("the real handle did not lift the named row")
	}
}

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

func TestRailStateDecisionPreservesLatestCollapseSnapshot(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"reveal", "rename"} {
		t.Run(scenario, func(t *testing.T) {
			m := buildModel(t)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"initial"}}}, 0, false)
			m.nextEffectCmd()
			m.rail.SetCollapsed("parent", true)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: m.rail.Collapsed()}}, 0, false)
			var expected []string
			if scenario == "rename" {
				m.queueRail(m.rail.RenameGroup("parent", "renamed").Mutations, 0, true)
				expected = []string{"renamed"}
			} else {
				m.queueRail(m.rail.RevealGroup("parent/child").Mutations, 0, true)
			}
			m.drainEffects(t)
			raw, err := m.services.store.Setting(uirailpersist.CollapsedSetting)
			if err != nil {
				t.Fatal(err)
			}
			var stored []string
			if err := json.Unmarshal([]byte(raw), &stored); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(stored, expected) {
				t.Fatalf("latest %s preference overwritten: got %v, want %v", scenario, stored, expected)
			}
		})
	}
}

func TestPrioritizedCollapseSaveKeepsActiveAndUnrelatedJobs(t *testing.T) {
	m := &Model{}
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.nextEffectCmd()
	active := m.effects.Main.Active
	m.enqueueEffect(uigeometrypanes.ResizeRequest{}, 0, false)
	geometry := m.effects.Main.Pending[0]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.enqueueEffect(uilifecycle.Request{}, 0, false)
	lifecycle := m.effects.Main.Pending[2]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"latest"}}}, 0, true)
	if m.effects.Main.Active != active || len(m.effects.Main.Pending) != 3 || m.effects.Main.Pending[1] != geometry || m.effects.Main.Pending[2] != lifecycle {
		t.Fatal("superseding snapshots changed active work or unrelated job order")
	}
}

func withGroups(t *testing.T, m *Model, dir string, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := m.services.store.CreateGroup(path, dir); err != nil {
			t.Fatalf("group %s: %v", path, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
}

func dragOnto(t *testing.T, m *Model, from treeRow, line int) (*Model, tea.MouseMsg) {
	t.Helper()
	preparedView(m)
	y0, _ := m.bodyYRange()
	start := -1
	for index, row := range railHitRows(m) {
		if row >= 0 && rowKey(railRows(m)[row]) == rowKey(from) {
			start = index
			break
		}
	}
	if start < 0 {
		t.Fatalf("test setup: %s is not painted", rowKey(from))
	}
	press := tea.MouseMsg{X: m.layout.displayedRail.Handles[rowKey(from)], Y: y0 + start, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	move := press
	move.Y, move.Action = y0+line, tea.MouseActionMotion
	updated, _ = m.handleMouse(move)
	m.drainEffects(t)
	return updated.(*Model), move
}

func release(t *testing.T, m *Model, at tea.MouseMsg) *Model {
	t.Helper()
	at.Action = tea.MouseActionRelease
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.applyCmd(t, m.refreshCmd())
	return m
}

func TestDragOntoAnotherGroupsHeaderMovesTheSessionInto(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	createSession(t, m, "stay", dir, "beta")

	m, at := dragOnto(t, m, sessionRow(t, m, "mover"), paintedGroupLines(t, m, "beta")[0])
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "into beta") {
		t.Fatalf("footer should name the pending move: %q", footer)
	}
	if got, _ := m.services.store.Get(sessionRow(t, m, "mover").sess.ID); got.Group != "alpha" {
		t.Fatalf("nothing is stored before release, group = %q", got.Group)
	}
	m = release(t, m, at)
	if got := sessionRow(t, m, "mover").sess; got.Group != "beta" || m.rail.Reordering() {
		t.Fatalf("release should move mover into beta, group = %q reorder = %v", got.Group, m.rail.Reordering())
	}
}

func TestDragBeforeASessionInAnotherGroupLandsAheadOfIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	createSession(t, m, "first", dir, "beta")
	createSession(t, m, "second", dir, "beta")

	m, at := dragOnto(t, m, sessionRow(t, m, "mover"), paintedRailLines(t, m, "second")[0])
	m = release(t, m, at)
	var beta []string
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.Group == "beta" {
			beta = append(beta, row.sess.Name)
		}
	}
	if strings.Join(beta, ",") != "first,mover,second" {
		t.Fatalf("beta should read first, mover, second, got %v", beta)
	}
}

func TestDragATerminalOntoAnAgentNestsIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "agent", dir, "beta")
	m.selectGroupRow(t, "alpha")
	shell := spawnTerminal(t, m)

	m, at := dragOnto(t, m, sessionRow(t, m, shell.Name), paintedRailLines(t, m, "agent")[0])
	m = release(t, m, at)
	agent := sessionRow(t, m, "agent").sess
	if got := sessionRow(t, m, shell.Name).sess; got.ParentID != agent.ID || got.Group != "beta" {
		t.Fatalf("terminal should sit under agent in beta, got parent %q group %q", got.ParentID, got.Group)
	}
}

func TestDragAGroupIntoAnotherGroupThroughItsRows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "inside", dir, "beta")
	var alpha treeRow
	for _, row := range railRows(m) {
		if row.isGroup && row.group == "alpha" {
			alpha = row
		}
	}
	m, at := dragOnto(t, m, alpha, paintedRailLines(t, m, "inside")[0])
	m = release(t, m, at)
	for _, row := range railRows(m) {
		if row.isGroup && row.group == "beta/alpha" {
			return
		}
	}
	t.Fatal("alpha should now live at beta/alpha")
}

func TestEscCancelsAPendingMove(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	m, _ = dragOnto(t, m, sessionRow(t, m, "mover"), paintedGroupLines(t, m, "beta")[0])
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	m.applyCmd(t, m.refreshCmd())
	if got := sessionRow(t, m, "mover").sess; got.Group != "alpha" || m.rail.Reordering() {
		t.Fatalf("esc should leave mover in alpha, group = %q", got.Group)
	}
}

func TestKeptSearchTakesNoCrossLevelMoves(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	createSession(t, m, "stay", dir, "beta")
	m.rail.SetSearch("e", false)
	m, at := dragOnto(t, m, sessionRow(t, m, "mover"), paintedGroupLines(t, m, "beta")[0])
	if footer := ansi.Strip(m.viewFooter()); strings.Contains(footer, "move into") {
		t.Fatalf("filtered drag offered a cross-level move: %q", footer)
	}
	m = release(t, m, at)
	if got := sessionRow(t, m, "mover").sess; got.Group != "alpha" {
		t.Fatalf("filtered drag moved across levels, group = %q", got.Group)
	}
}

// tallRail fills the rail past its window, so the last painted line is an
// edge a drag can rest on.
func tallRail(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	dir := t.TempDir()
	// Autoscroll needs only more rows than the rail is tall; stored rows
	// paint that tree without forty tmux sessions behind it.
	for i := 0; i < 40; i++ {
		sess := store.Session{ID: fmt.Sprintf("tall%02d", i), Name: fmt.Sprintf("s%02d", i), Tool: "claude", Cwd: dir, Status: status.Idle}
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create %s: %v", sess.Name, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "s00")
	return m
}

// dragToBottomEdge lifts name and rests the drag on the last painted rail
// line, returning the autoscroll tick the root scheduled for it.
func dragToBottomEdge(t *testing.T, m *Model, name string) (*Model, uirail.AutoScrollTick, bool) {
	t.Helper()
	m = liftByHandle(t, m, name)
	preparedView(m)
	last := -1
	for i, row := range railHitRows(m) {
		if row >= 0 {
			last = i
		}
	}
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + last, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = updated.(*Model)
	tick, ok := m.takeAutoscrollTick(t, cmd)
	m.drainEffects(t)
	return m, tick, ok
}

// takeAutoscrollTick runs cmd the way the program would, delivering every
// message except the autoscroll tick, which it hands back undelivered.
func (m *Model) takeAutoscrollTick(t *testing.T, cmd tea.Cmd) (uirail.AutoScrollTick, bool) {
	t.Helper()
	if cmd == nil {
		return uirail.AutoScrollTick{}, false
	}
	switch msg := cmd().(type) {
	case uirail.AutoScrollTick:
		return msg, true
	case tea.BatchMsg:
		var found uirail.AutoScrollTick
		ok := false
		for _, next := range msg {
			if tick, got := m.takeAutoscrollTick(t, next); got {
				found, ok = tick, true
			}
		}
		return found, ok
	default:
		m.applyTestMsg(t, msg)
		return uirail.AutoScrollTick{}, false
	}
}

func TestDragRestingOnTheBottomEdgeScrollsTheRail(t *testing.T) {
	m := tallRail(t)
	m, tick, ok := dragToBottomEdge(t, m, "s00")
	if !ok {
		t.Fatal("resting on the last rail line should start the scroll")
	}
	preparedView(m)
	before := m.layout.displayedRail.Window.Start
	for i := 0; i < 3; i++ {
		updated, _ := m.Update(tick)
		m = updated.(*Model)
		m.drainEffects(t)
		preparedView(m)
	}
	if after := m.layout.displayedRail.Window.Start; after <= before {
		t.Fatalf("the rail should have scrolled down, top %d then %d", before, after)
	}
}

// A tick scheduled by one drag must not steer the next: the next drag runs
// its own tick, and a second one would scroll it twice as fast.
func TestAStaleAutoscrollTickLeavesTheNextDragAlone(t *testing.T) {
	m := tallRail(t)
	m, stale, ok := dragToBottomEdge(t, m, "s00")
	if !ok {
		t.Fatal("test setup: the first drag scheduled no tick")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	m.applyCmd(t, m.refreshCmd())
	if m.rail.Reordering() {
		t.Fatal("test setup: esc should end the first drag")
	}
	m, _, ok = dragToBottomEdge(t, m, "s00")
	if !ok {
		t.Fatal("test setup: the second drag scheduled no tick")
	}
	preparedView(m)
	before := m.layout.displayedRail.Window.Start
	updated, cmd := m.Update(stale)
	m = updated.(*Model)
	preparedView(m)
	if cmd != nil || !m.rail.Reordering() || m.layout.displayedRail.Window.Start != before {
		t.Fatalf("a stale tick should change nothing, cmd = %v reordering = %v top %d then %d",
			cmd != nil, m.rail.Reordering(), before, m.layout.displayedRail.Window.Start)
	}
}

func TestPortableReorderKeysSwapVisibleSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "keep-charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.rail.SetSearch("keep", m.rail.Searching())
	loadStoredRows(t, m)
	m.selectSessionRow(t, "keep-charlie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if got, want := []string{m.sessionRows()[0].ID, m.sessionRows()[1].ID}, []string{"c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("visible order after K = %v want %v", got, want)
	}
	if got, want := listSessionIDs(t, m.services.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after K = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.services.store), []string{"a", "hidden", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after J = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	m.drainEffects(t)
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.services.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after shift+up = %v want %v", got, want)
	}
}

func TestReorderGroupSkipsFilteredSibling(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, group := range []string{"alpha", "hidden", "gamma"} {
		if err := m.services.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Group: "alpha", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Group: "hidden", Status: "idle"},
		{ID: "g", Name: "keep-gamma", Tool: "claude", Cwd: "/tmp", Group: "gamma", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.rail.SetSearch("keep", m.rail.Searching())
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if got, want := m.groupRowPaths(), []string{"gamma", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("visible group order after K = %v want %v", got, want)
	}
	groups, err := m.services.store.Groups()
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
	t.Parallel()
	m := buildModel(t)
	for _, group := range []string{"alpha/deep", "beta/deep", "gamma/deep"} {
		if err := m.services.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m.drainEffects(t)
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

func TestReorderChildStaysWithItsSiblings(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	first := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	second := spawnTerminal(t, m)
	m.selectSessionRow(t, first.Name)
	updated, _ := m.handleKey(runeKey("J"))
	m.drainEffects(t)
	m = updated.(*Model)
	var kids []string
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.ParentID != "" {
			kids = append(kids, row.sess.Name)
		}
	}
	if len(kids) != 2 || kids[0] != second.Name || kids[1] != first.Name {
		t.Fatalf("sibling order = %v", kids)
	}
}

func TestReorderChildIgnoresAnotherParentsChild(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	mine := spawnTerminal(t, m)
	m.selectSessionRow(t, "other")
	theirs := spawnTerminal(t, m)
	m.selectSessionRow(t, mine.Name)
	updated, _ := m.handleKey(runeKey("J"))
	m.drainEffects(t)
	m = updated.(*Model)
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	want := []string{"coder", mine.Name, "other", theirs.Name}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestReorderAgentSkipsChildren(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	agent := m.sessionRows()[0]
	updated, _ := m.handleKey(runeKey("J"))
	m.drainEffects(t)
	m = updated.(*Model)
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.ParentID == "" {
			names = append(names, row.sess.Name)
		}
	}
	if len(names) < 2 || names[0] != "other" || names[1] != "coder" {
		t.Fatalf("un-nested order %v", names)
	}
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("terminal left its parent: %+v err %v", got, err)
	}
}

func TestHandleLiftsARowStraightFromFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	m = liftByHandle(t, m, "beta")
	if selected, ok := m.selected(); !m.onList() || !m.rail.Reordering() || !ok || selected.Name != "beta" {
		t.Fatalf("one press on beta's handle should leave focus and lift beta, screen = %v selected = %q", m.screenID(), selected.Name)
	}
}

func liftByHandle(t *testing.T, m *Model, name string) *Model {
	t.Helper()
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, name)[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Handles[rowKey(railRows(m)[railHitRows(m)[line]])], Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.drainEffects(t)
	m = updated.(*Model)
	if !m.rail.Reordering() {
		t.Fatalf("a press on the handle should lift %s", name)
	}
	return m
}

func sessionOrder(m *Model) []string {
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	return names
}

func TestEveryMovableRowPaintsItsHandle(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	frame := strings.Split(ansi.Strip(preparedView(m)), "\n")
	for _, name := range []string{"alpha", "beta"} {
		y0, _ := m.bodyYRange()
		row := frame[y0+paintedRailLines(t, m, name)[0]]
		if !strings.Contains(row, reorderGrip+" "+name) {
			t.Fatalf("%s should carry the handle right before its name, got %q", name, row)
		}
	}
}

func TestHandleDragReordersAndDrops(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	before := sessionOrder(m)
	m = liftByHandle(t, m, before[0])
	m = railMouse(t, m, before[2], tea.MouseActionMotion, tea.MouseButtonLeft)
	if got := sessionOrder(m); got[2] != before[0] {
		t.Fatalf("dragging onto the last row should move %s there, got %v", before[0], got)
	}
	m = railMouse(t, m, before[0], tea.MouseActionRelease, tea.MouseButtonLeft)
	if m.rail.Reordering() || !m.onList() {
		t.Fatalf("release after a drag should drop the row, reorder = %v screen = %v", m.rail.Reordering(), m.screenID())
	}
}

func TestHandleThenKeysAndEscPutsItBack(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	before := sessionOrder(m)
	m = liftByHandle(t, m, before[0])
	m = railMouse(t, m, before[0], tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.rail.Reordering() || !m.onList() {
		t.Fatalf("a release in place keeps the row lifted and unfocused, reorder = %v screen = %v", m.rail.Reordering(), m.screenID())
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.drainEffects(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.drainEffects(t)
	if got := sessionOrder(m); got[2] != before[0] {
		t.Fatalf("down twice should move %s last, got %v", before[0], got)
	}
	preparedView(m)
	if !strings.Contains(ansi.Strip(m.viewFooter()), "Reorder") {
		t.Fatalf("footer should name the reorder mode:\n%s", ansi.Strip(m.viewFooter()))
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	if got := sessionOrder(m); strings.Join(got, ",") != strings.Join(before, ",") || m.rail.Reordering() {
		t.Fatalf("esc should put the row back, got %v want %v", got, before)
	}
}

func TestAnotherHandleTakesOverALiftedRowInOnePress(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	m = railMouse(t, m, "alpha", tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.rail.Reordering() {
		t.Fatal("test setup: alpha should stay lifted for the keyboard")
	}
	m = liftByHandle(t, m, "beta")
	if selected, ok := m.selected(); !m.rail.Reordering() || !ok || selected.Name != "beta" {
		t.Fatalf("one press on beta's handle should lift beta, selected = %q", selected.Name)
	}
}

func TestAPressOnALiftedRowsLabelPutsItDownAndClicks(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	m = railMouse(t, m, "alpha", tea.MouseActionRelease, tea.MouseButtonLeft)
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: m.layout.displayedRail.Handles["s:"+sessionRow(t, m, "alpha").sess.ID] + 4, Y: y0 + paintedRailLines(t, m, "alpha")[0], Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionPress
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	if m.rail.Reordering() || !m.focused() {
		t.Fatalf("a label click should drop the row and focus it, reorder = %v screen = %v", m.rail.Reordering(), m.screenID())
	}
}

// Lifting a row selects it, and its preview has to follow: the handle's
// press is the only event before the drag, so its fetch cannot be dropped.
func TestLiftingARowFetchesItsPreview(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, "beta")[0]
	_, cmd := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Handles["s:"+sessionRow(t, m, "beta").sess.ID], Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.drainEffects(t)
	if !m.rail.Reordering() || cmd == nil {
		t.Fatalf("lifting beta should schedule its preview, reorder = %v cmd = %v", m.rail.Reordering(), cmd != nil)
	}
}

// The handle sits on a row's first line only: in a comfortable row the
// prompt and reply lines below it are the row's label, which a click focuses.
func TestHandleColumnBelowTheFirstLineIsTheLabel(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.comfortableRows = true
	createSession(t, m, "alpha", t.TempDir(), "")
	preparedView(m)
	y0, _ := m.bodyYRange()
	lines := paintedRailLines(t, m, "alpha")
	if len(lines) < 2 {
		t.Fatalf("test setup: a comfortable row paints more than one line, got %v", lines)
	}
	at := tea.MouseMsg{X: m.layout.displayedRail.Handles[rowKey(sessionRow(t, m, "alpha"))], Y: y0 + lines[1], Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	if m.rail.Reordering() {
		t.Fatal("a press below the first line should not lift the row")
	}
}
