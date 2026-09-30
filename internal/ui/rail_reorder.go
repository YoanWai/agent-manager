package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// reorderSelected moves the selected session among its group siblings,
// or the selected group among the groups sharing its parent.
func (m *Model) reorderSelected(delta int) (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isRoot() {
		m.errBar.text = "root stays at the top of the list"
		return m, nil
	}
	target, ok := m.visibleReorderTarget(entry, delta)
	if !ok {
		edge := "top"
		if delta > 0 {
			edge = "bottom"
		}
		what := "group"
		if !entry.isGroup {
			what = "session"
		}
		m.errBar.text = fmt.Sprintf("%s already at the %s of its level", what, edge)
		return m, nil
	}
	if err := m.swapRows(entry, target); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.errBar.text = ""
	return m, nil
}

// swapRows trades places between a row and a visible sibling, in the store
// and in memory.
func (m *Model) swapRows(entry, target treeRow) error {
	var err error
	var groupSiblings []string
	if entry.isGroup {
		groupSiblings = m.knownGroupSiblings(parentGroup(entry.group))
		err = m.services.store.SwapGroupOrder(entry.group, target.group, groupSiblings...)
	} else {
		err = m.services.store.SwapSessionOrder(entry.sess.ID, target.sess.ID)
	}
	if err != nil {
		return err
	}
	// Mirror the swap in memory so the list redraws instantly; the next
	// poll re-reads the authoritative order from the store.
	if entry.isGroup {
		m.materializeGroupsLocal(groupSiblings)
		m.swapGroupLocal(entry.group, target.group)
	} else {
		m.swapSessionLocal(entry.sess.ID, target.sess.ID)
	}
	m.rebuildRows()
	m.requestRefresh()
	return nil
}

// visibleReorderTarget finds the next rendered sibling. Filters and archive
// scope therefore cannot turn a successful reorder into an invisible swap.
func (m *Model) visibleReorderTarget(entry treeRow, delta int) (treeRow, bool) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := m.rail.cursor + step; i >= 0 && i < len(m.rail.rows); i += step {
		candidate := m.rail.rows[i]
		if candidate.isRoot() {
			// parentGroup("") is "" too, so root would match a top-level
			// group as its own sibling.
			continue
		}
		if entry.isGroup {
			if candidate.isGroup && parentGroup(candidate.group) == parentGroup(entry.group) {
				return candidate, true
			}
			continue
		}
		if !candidate.isGroup && candidate.sess.Group == entry.sess.Group && candidate.sess.ParentID == entry.sess.ParentID {
			return candidate, true
		}
	}
	return treeRow{}, false
}

func (m *Model) knownGroupSiblings(parent string) []string {
	paths := groupClosure(m.workspace.groups, m.workspace.sessions)
	return childIndex(paths, m.workspace.groups)[parent]
}

func (m *Model) materializeGroupsLocal(paths []string) {
	known := make(map[string]bool, len(m.workspace.groups))
	for _, group := range m.workspace.groups {
		known[group] = true
	}
	for _, path := range paths {
		if !known[path] {
			m.workspace.groups = append(m.workspace.groups, path)
			known[path] = true
		}
	}
}

func (m *Model) swapSessionLocal(id, targetID string) {
	current, target := -1, -1
	for i, sess := range m.workspace.sessions {
		switch sess.ID {
		case id:
			current = i
		case targetID:
			target = i
		}
	}
	if current >= 0 && target >= 0 {
		m.workspace.sessions[current], m.workspace.sessions[target] = m.workspace.sessions[target], m.workspace.sessions[current]
	}
}

func (m *Model) swapGroupLocal(path, targetPath string) {
	current, target := -1, -1
	for i, name := range m.workspace.groups {
		switch name {
		case path:
			current = i
		case targetPath:
			target = i
		}
	}
	if current >= 0 && target >= 0 {
		m.workspace.groups[current], m.workspace.groups[target] = m.workspace.groups[target], m.workspace.groups[current]
	}
}

// reorderGrip is the drag handle every movable row carries beside its name.
const reorderGrip = "⠿"

// reorderState is a row lifted by its handle. Steps among its siblings are
// stored as they happen, so offset is what esc walks back. A move to
// another level waits in drop until the release.
type reorderState struct {
	active     bool
	lift       int
	key        string
	dragging   bool
	moved      bool
	offset     int
	drop       *dropTarget
	autoscroll autoscrollState
}

// onHandle reports whether a press at (x, y) on row grabs its handle,
// counting the gap on either side so the target is wider than one cell.
func (m *Model) onHandle(x, y, row int) bool {
	handle, ok := m.rail.handleX[rowKey(m.rail.rows[row])]
	return ok && x >= handle-1 && x <= handle+1 && m.onRowHead(y, row)
}

// rowHandle is the grip a movable row carries after lead. The row hides it
// while renamed and while the mouse is off.
func (m *Model) rowHandle(entry treeRow, selected bool) string {
	if entry.isRoot() || m.renamingRow(entry) || m.prefs.mouseDisabled {
		return ""
	}
	return m.handleGlyph(entry, selected) + " "
}

// handleGlyph is the grip as the row paints it: quiet at rest, brighter
// under the cursor, the accent while lifted.
func (m *Model) handleGlyph(entry treeRow, selected bool) string {
	switch {
	case m.liftedRow(entry):
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(reorderGrip)
	case selected:
		return lipgloss.NewStyle().Foreground(colorDim).Render(reorderGrip)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(restingMarkHex())).Render(reorderGrip)
}

func (m *Model) liftedRow(entry treeRow) bool {
	return m.rail.reorder.active && rowKey(entry) == m.rail.reorder.key
}

func (m *Model) enterReorder(key string) {
	m.rail.lifts++
	m.rail.menu = rowMenu{}
	m.errBar.text = ""
	m.rail.reorder = reorderState{active: true, lift: m.rail.lifts, key: key, dragging: true}
}

// exitReorder puts the lifted row down where it stands, or with keep false
// back where it was lifted from.
func (m *Model) exitReorder(keep bool) {
	for !keep && m.rail.reorder.offset != 0 {
		back := -1
		if m.rail.reorder.offset < 0 {
			back = 1
		}
		if !m.reorderStep(back) {
			break
		}
	}
	m.rail.reorder = reorderState{}
}

// reorderStep moves the lifted row one visible sibling over, quietly
// refusing at either end of its level or once the row has left the list.
func (m *Model) reorderStep(delta int) bool {
	entry, ok := m.selectedRow()
	if !ok || rowKey(entry) != m.rail.reorder.key {
		return false
	}
	target, ok := m.visibleReorderTarget(entry, delta)
	if !ok {
		return false
	}
	if err := m.swapRows(entry, target); err != nil {
		m.errBar.text = err.Error()
		return false
	}
	m.rail.reorder.offset += delta
	m.rail.reorder.moved = true
	return true
}

// stepLifted moves the lifted row from the keyboard or the wheel, which
// drops any pending move and hands the rail window back to the cursor.
func (m *Model) stepLifted(delta int) {
	m.rail.reorder.drop = nil
	m.rail.reorder.autoscroll.anchored = false
	m.reorderStep(delta)
}

// dragReorderTo steps the lifted row toward the row under the pointer,
// one sibling at a time, until the next sibling lies past the pointer.
func (m *Model) dragReorderTo(target int) {
	for m.rail.cursor != target {
		entry, ok := m.selectedRow()
		if !ok {
			return
		}
		delta := 1
		if target < m.rail.cursor {
			delta = -1
		}
		next, ok := m.visibleReorderTarget(entry, delta)
		if !ok {
			return
		}
		nextIndex := m.rowIndexByKey(rowKey(next))
		if (delta > 0 && nextIndex > target) || (delta < 0 && nextIndex < target) {
			return
		}
		if !m.reorderStep(delta) {
			return
		}
	}
}

func (m *Model) rowIndexByKey(key string) int {
	for i, row := range m.rail.rows {
		if rowKey(row) == key {
			return i
		}
	}
	return -1
}

func (m *Model) handleReorderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := keybind.Normalize(msg.String())
	action, _ := m.services.listKeys.ActionFor(key)
	switch {
	case key == "ctrl+c" || action == keybind.Quit:
		m.exitReorder(true)
		return m, tea.Quit
	case key == "enter" || key == "space":
		m.exitReorder(true)
	case key == "esc":
		m.exitReorder(false)
	case key == "up" || action == keybind.Up || action == keybind.ReorderUp:
		m.stepLifted(-1)
	case key == "down" || action == keybind.Down || action == keybind.ReorderDown:
		m.stepLifted(1)
	}
	return m, nil
}

// handleReorderMouse drives a lifted row: dragging follows the pointer, a
// release after a drag drops it, and a release in place keeps it lifted
// for the keyboard. A press on its own handle picks it up again; any other
// press puts it down and then does what it would have done anyway, so
// another row's handle starts that row's drag in the same press.
func (m *Model) handleReorderMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if tea.MouseEvent(msg).IsWheel() {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.stepLifted(-1)
		case tea.MouseButtonWheelDown:
			m.stepLifted(1)
		}
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionMotion:
		if !m.rail.reorder.dragging {
			return m, nil
		}
		if row, ok := m.clickRow(msg.X, msg.Y); ok {
			m.dragTo(row)
		}
		return m, m.trackDragEdge(msg)
	case tea.MouseActionRelease:
		if !m.rail.reorder.dragging {
			return m, nil
		}
		m.rail.reorder.dragging = false
		switch {
		case m.rail.reorder.drop != nil:
			m.commitDrop(*m.rail.reorder.drop)
			m.exitReorder(true)
		case m.rail.reorder.moved:
			m.exitReorder(true)
		default:
			m.rail.reorder.autoscroll.anchored = false
		}
	case tea.MouseActionPress:
		row, onRail := m.clickRow(msg.X, msg.Y)
		if onRail && msg.Button == tea.MouseButtonLeft && rowKey(m.rail.rows[row]) == m.rail.reorder.key && m.onHandle(msg.X, msg.Y, row) {
			m.rail.reorder.dragging = true
			return m, nil
		}
		m.exitReorder(true)
		return m.handleMouseEvent(msg)
	}
	return m, nil
}

func (m *Model) reorderFooter() string {
	if drop := m.rail.reorder.drop; drop != nil {
		return m.transientFooter(legendSection{title: "Move", pairs: [][2]string{
			{"release", "move " + drop.label}, {"esc", "put back"},
		}})
	}
	return m.transientFooter(legendSection{title: "Reorder", pairs: [][2]string{
		{"drag / ↑↓ / wheel", "move"}, {"↵ / release", "drop"}, {"esc", "put back"},
	}})
}
