package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// dropKind is where a row dragged out of its own level lands.
type dropKind int

const (
	// dropInto puts a session at a group's top level, or a group under one.
	dropInto dropKind = iota + 1
	// dropUnder nests a terminal under an agent.
	dropUnder
	// dropBefore puts a session among another level's sessions, ahead of one.
	dropBefore
)

// dropTarget is a move waiting for the release: nothing is stored while
// the pointer hovers, so esc or a drag back home leaves the list as it was.
// key is the row painted as the landing spot.
type dropTarget struct {
	kind  dropKind
	key   string
	group string
	id    string
	label string
}

// resolveDrop names the move the row under the pointer stands for. A row on
// the lifted row's own level resolves to nothing: that is a reorder, which
// happens live. Filtered views hide rows, so they take no cross-level moves.
func (m *Model) resolveDrop(row int) (dropTarget, bool) {
	if m.showArchived || m.statusFilter.active() || m.search != "" {
		return dropTarget{}, false
	}
	liftedIndex := m.rowIndexByKey(m.reorder.key)
	target := m.rows[row]
	if liftedIndex < 0 || rowKey(target) == m.reorder.key {
		return dropTarget{}, false
	}
	lifted := m.rows[liftedIndex]
	if lifted.isGroup {
		return resolveGroupDrop(lifted.group, target)
	}
	return m.resolveSessionDrop(lifted.sess, target)
}

func (m *Model) resolveSessionDrop(sess store.Session, target treeRow) (dropTarget, bool) {
	if target.isGroup {
		if target.group == sess.Group && sess.ParentID == "" {
			return dropTarget{}, false
		}
		return dropTarget{kind: dropInto, key: rowKey(target), group: target.group, label: "into " + groupLabel(target.group)}, true
	}
	other := target.sess
	if other.ID == sess.ParentID || other.ParentID == sess.ID {
		return dropTarget{}, false
	}
	if m.isShell(sess.Tool) && !m.isShell(other.Tool) && other.ParentID == "" {
		return dropTarget{kind: dropUnder, key: rowKey(target), group: other.Group, id: other.ID, label: "under " + m.displayName(other)}, true
	}
	// Only terminals nest, so an agent lands beside the agent a terminal
	// belongs to.
	if !m.isShell(sess.Tool) && other.ParentID != "" {
		parent, ok := m.sessionByID(other.ParentID)
		if !ok {
			return dropTarget{}, false
		}
		other = parent
	}
	if other.Group == sess.Group && other.ParentID == sess.ParentID {
		return dropTarget{}, false
	}
	return dropTarget{
		kind: dropBefore, key: rowKey(treeRow{sess: other}), group: other.Group, id: other.ID,
		label: "before " + m.displayName(other) + " in " + groupLabel(other.Group),
	}, true
}

// resolveGroupDrop moves a group under the group the pointer is in. A
// sibling group's header is a reorder instead, so a group lands inside a
// sibling by hovering one of that sibling's rows.
func resolveGroupDrop(path string, target treeRow) (dropTarget, bool) {
	dest := target.group
	if !target.isGroup {
		dest = target.sess.Group
	} else if !target.isRoot() && parentGroup(target.group) == parentGroup(path) {
		return dropTarget{}, false
	}
	if dest == parentGroup(path) || inGroupSubtree(dest, path) {
		return dropTarget{}, false
	}
	key := rowKey(treeRow{isGroup: true, group: dest})
	return dropTarget{kind: dropInto, key: key, group: dest, label: "into " + groupLabel(dest)}, true
}

func groupLabel(path string) string {
	if path == "" {
		return "root"
	}
	return baseName(path)
}

func (m *Model) sessionByID(id string) (store.Session, bool) {
	for _, sess := range m.sessions {
		if sess.ID == id {
			return sess, true
		}
	}
	return store.Session{}, false
}

// dragTo follows the pointer: a row on the lifted row's level reorders it
// live, any other row stands for a move that waits for the release.
func (m *Model) dragTo(row int) {
	if drop, ok := m.resolveDrop(row); ok {
		m.reorder.drop = &drop
		return
	}
	m.reorder.drop = nil
	m.dragReorderTo(row)
}

// commitDrop stores the move the release landed on and opens the group it
// went into, so the row is on screen where it was put.
func (m *Model) commitDrop(drop dropTarget) {
	liftedIndex := m.rowIndexByKey(m.reorder.key)
	if liftedIndex < 0 {
		return
	}
	lifted := m.rows[liftedIndex]
	var err error
	switch {
	case lifted.isGroup:
		err = m.moveGroupUnder(lifted.group, drop.group)
	case drop.kind == dropInto:
		err = m.store.PlaceSession(lifted.sess.ID, drop.group, "")
	case drop.kind == dropUnder:
		err = m.store.PlaceSession(lifted.sess.ID, drop.group, drop.id)
	case drop.kind == dropBefore:
		err = m.store.PlaceSessionBefore(lifted.sess.ID, drop.id)
	}
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if !lifted.isGroup {
		m.relabelMoved(lifted.sess)
	}
	if m.collapsed[drop.group] {
		m.collapsed[drop.group] = false
		m.persistCollapsed()
	}
	m.requestRefresh()
}

// relabelMoved refreshes the tmux labels of a moved session and of the
// terminals that moved with it, since a label names the group.
func (m *Model) relabelMoved(sess store.Session) {
	moved, err := m.sessionAndChildren(sess)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	for _, each := range moved {
		m.relabelSession(each.ID)
	}
}

func (m *Model) dropRow(entry treeRow) bool {
	return m.reorder.active && m.reorder.drop != nil && rowKey(entry) == m.reorder.drop.key
}

// autoscrollEvery is how often the rail steps while a drag rests on its edge.
const autoscrollEvery = 90 * time.Millisecond

// autoscrollState scrolls the rail while a drag rests on its top or bottom
// row: edge is the direction, x and y where the pointer rests, and anchor
// the row the rail window keeps on screen instead of the cursor.
type autoscrollState struct {
	edge     int
	running  bool
	x, y     int
	anchor   int
	anchored bool
}

// autoscrollMsg carries the lift it was started for, so a tick left over
// from an earlier drag cannot scroll the next one.
type autoscrollMsg struct{ lift int }

// dragEdge reports which edge of the rail's rows the pointer rests on, while
// more rows lie beyond that edge: -1 the top, 1 the bottom, 0 neither.
func (m *Model) dragEdge(y int) int {
	y0, _ := m.bodyYRange()
	line := y - y0
	first, last := -1, -1
	for i, row := range m.railHits {
		if row >= 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	switch {
	case first >= 0 && line <= first && m.railTop > 0:
		return -1
	case last >= 0 && line >= last && m.railEnd < len(m.rows):
		return 1
	}
	return 0
}

// trackDragEdge starts the autoscroll tick when the pointer reaches an
// edge and lets it lapse once the pointer leaves.
func (m *Model) trackDragEdge(msg tea.MouseMsg) tea.Cmd {
	scroll := &m.reorder.autoscroll
	scroll.x, scroll.y = msg.X, msg.Y
	scroll.edge = m.dragEdge(msg.Y)
	if scroll.edge == 0 || scroll.running {
		return nil
	}
	scroll.running = true
	return autoscrollTick(m.reorder.lift)
}

func autoscrollTick(lift int) tea.Cmd {
	return tea.Tick(autoscrollEvery, func(time.Time) tea.Msg { return autoscrollMsg{lift: lift} })
}

// handleAutoscroll steps the rail one row toward the edge the drag rests
// on, then re-reads the row now under the still pointer.
func (m *Model) handleAutoscroll(msg autoscrollMsg) (tea.Model, tea.Cmd) {
	if msg.lift != m.reorder.lift {
		return m, nil
	}
	scroll := &m.reorder.autoscroll
	if !m.reorder.dragging || scroll.edge == 0 {
		scroll.running = false
		return m, nil
	}
	if row, ok := m.clickRow(scroll.x, scroll.y); ok {
		m.dragTo(row)
	}
	next := m.railTop - 1
	if scroll.edge > 0 {
		next = m.railEnd
	}
	if next < 0 || next >= len(m.rows) {
		scroll.running = false
		return m, nil
	}
	scroll.anchor, scroll.anchored = next, true
	scroll.edge = m.dragEdge(scroll.y)
	return m, autoscrollTick(msg.lift)
}

// railAnchor is the row the rail window keeps on screen: the cursor, or
// while a drag scrolls the rail, the row the scroll reached.
func (m *Model) railAnchor() int {
	if m.reorder.active && m.reorder.autoscroll.anchored {
		return m.reorder.autoscroll.anchor
	}
	return m.cursor
}
