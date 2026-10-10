package ui

import (
	"fmt"
	"strings"
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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
	for i := 0; i < 40; i++ {
		createSession(t, m, fmt.Sprintf("s%02d", i), t.TempDir(), "")
	}
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
