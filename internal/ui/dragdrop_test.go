package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func withGroups(t *testing.T, m *Model, dir string, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := m.store.CreateGroup(path, dir); err != nil {
			t.Fatalf("group %s: %v", path, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
}

// dragOnto lifts a row by its handle and drags it to a painted rail line,
// still holding it there.
func dragOnto(t *testing.T, m *Model, from treeRow, line int) *Model {
	t.Helper()
	m.View()
	y0, _ := m.bodyYRange()
	start := slices.IndexFunc(m.railHits, func(row int) bool { return row >= 0 && rowKey(m.rows[row]) == rowKey(from) })
	if start < 0 {
		t.Fatalf("test setup: %s is not painted", rowKey(from))
	}
	at := tea.MouseMsg{X: m.handleX[rowKey(from)], Y: y0 + start, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	at.Y, at.Action = y0+line, tea.MouseActionMotion
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	return m
}

// release lets go where the drag rests, then refreshes the list the way
// the poll would.
func release(t *testing.T, m *Model) *Model {
	t.Helper()
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.reorder.autoscroll.x, Y: m.reorder.autoscroll.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = updated.(*Model)
	m.applyCmd(t, m.refreshCmd())
	return m
}

func TestDragOntoAnotherGroupsHeaderMovesTheSessionInto(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	createSession(t, m, "stay", dir, "beta")

	m = dragOnto(t, m, sessionRow(t, m, "mover"), paintedGroupLines(t, m, "beta")[0])
	if m.reorder.drop == nil || m.reorder.drop.kind != dropInto {
		t.Fatalf("hovering beta should stand for a move into it, drop = %+v", m.reorder.drop)
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "into beta") {
		t.Fatalf("footer should name the move: %q", footer)
	}
	if got, _ := m.store.Get(sessionRow(t, m, "mover").sess.ID); got.Group != "alpha" {
		t.Fatalf("nothing is stored before the release, group = %q", got.Group)
	}
	m = release(t, m)
	if got := sessionRow(t, m, "mover").sess; got.Group != "beta" || m.reorder.active {
		t.Fatalf("release should move mover into beta, group = %q reorder = %v", got.Group, m.reorder.active)
	}
}

func TestDragBeforeASessionInAnotherGroupLandsAheadOfIt(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	createSession(t, m, "first", dir, "beta")
	createSession(t, m, "second", dir, "beta")

	m = dragOnto(t, m, sessionRow(t, m, "mover"), paintedRailLines(t, m, "second")[0])
	if m.reorder.drop == nil || m.reorder.drop.kind != dropBefore {
		t.Fatalf("hovering second should stand for a move before it, drop = %+v", m.reorder.drop)
	}
	m = release(t, m)
	var beta []string
	for _, row := range m.rows {
		if !row.isGroup && row.sess.Group == "beta" {
			beta = append(beta, row.sess.Name)
		}
	}
	if strings.Join(beta, ",") != "first,mover,second" {
		t.Fatalf("beta should read first, mover, second, got %v", beta)
	}
}

func TestDragATerminalOntoAnAgentNestsIt(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "agent", dir, "beta")
	m.selectGroupRow(t, "alpha")
	shell := spawnTerminal(t, m)

	m = dragOnto(t, m, sessionRow(t, m, shell.Name), paintedRailLines(t, m, "agent")[0])
	if m.reorder.drop == nil || m.reorder.drop.kind != dropUnder {
		t.Fatalf("hovering an agent should stand for nesting under it, drop = %+v", m.reorder.drop)
	}
	m = release(t, m)
	agent := sessionRow(t, m, "agent").sess
	if got := sessionRow(t, m, shell.Name).sess; got.ParentID != agent.ID || got.Group != "beta" {
		t.Fatalf("the terminal should sit under agent in beta, got parent %q group %q", got.ParentID, got.Group)
	}
}

func TestDragAGroupIntoAnotherGroupThroughItsRows(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "inside", dir, "beta")
	var alpha treeRow
	for _, row := range m.rows {
		if row.isGroup && row.group == "alpha" {
			alpha = row
		}
	}
	m = dragOnto(t, m, alpha, paintedRailLines(t, m, "inside")[0])
	if m.reorder.drop == nil || m.reorder.drop.group != "beta" {
		t.Fatalf("hovering a row in beta should stand for moving alpha into beta, drop = %+v", m.reorder.drop)
	}
	m = release(t, m)
	found := false
	for _, row := range m.rows {
		found = found || (row.isGroup && row.group == "beta/alpha")
	}
	if !found {
		t.Fatal("alpha should now live at beta/alpha")
	}
}

func TestEscCancelsAPendingMove(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	m = dragOnto(t, m, sessionRow(t, m, "mover"), paintedGroupLines(t, m, "beta")[0])
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, m.refreshCmd())
	if got := sessionRow(t, m, "mover").sess; got.Group != "alpha" || m.reorder.active {
		t.Fatalf("esc should leave mover in alpha, group = %q", got.Group)
	}
}

func TestDragRestingOnTheBottomEdgeScrollsTheRail(t *testing.T) {
	m := buildModel(t)
	for i := 0; i < 40; i++ {
		createSession(t, m, fmt.Sprintf("s%02d", i), t.TempDir(), "")
	}
	m.selectSessionRow(t, "s00")
	m.View()
	last := -1
	for i, row := range m.railHits {
		if row >= 0 {
			last = i
		}
	}
	m = dragOnto(t, m, sessionRow(t, m, "s00"), last)
	if m.reorder.autoscroll.edge != 1 || !m.reorder.autoscroll.running {
		t.Fatalf("resting on the last rail line should start the scroll, edge = %d", m.reorder.autoscroll.edge)
	}
	before := m.railTop
	for i := 0; i < 3; i++ {
		updated, _ := m.handleAutoscroll(autoscrollMsg{lift: m.reorder.lift})
		m = updated.(*Model)
		m.View()
	}
	if m.railTop <= before {
		t.Fatalf("the rail should have scrolled down, top %d then %d", before, m.railTop)
	}
}

// A tick scheduled by one drag must not steer the next: the next drag runs
// its own tick, and a second one would scroll it twice as fast.
func TestAStaleAutoscrollTickLeavesTheNextDragAlone(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	stale := autoscrollMsg{lift: m.reorder.lift}
	m.exitReorder(true)
	m = liftByHandle(t, m, "alpha")
	m.reorder.autoscroll.edge, m.reorder.autoscroll.running = 1, true
	updated, cmd := m.handleAutoscroll(stale)
	m = updated.(*Model)
	if cmd != nil || !m.reorder.autoscroll.running || m.reorder.autoscroll.anchored {
		t.Fatalf("a stale tick should change nothing, cmd = %v scroll = %+v", cmd != nil, m.reorder.autoscroll)
	}
}

// A kept search hides rows as surely as a filter does, so the drop spot
// would be ambiguous: rows only reorder among their siblings there.
func TestKeptSearchTakesNoCrossLevelMoves(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	withGroups(t, m, dir, "alpha", "beta")
	createSession(t, m, "mover", dir, "alpha")
	createSession(t, m, "stay", dir, "beta")
	m.search = "e"
	m.rebuildRows()
	m = dragOnto(t, m, sessionRow(t, m, "mover"), paintedGroupLines(t, m, "beta")[0])
	if m.reorder.drop != nil {
		t.Fatalf("a kept search should take no cross-level move, drop = %+v", m.reorder.drop)
	}
}
