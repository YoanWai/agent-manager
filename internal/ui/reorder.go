package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

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
	handle, ok := m.handleX[rowKey(m.rows[row])]
	return ok && x >= handle-1 && x <= handle+1 && m.onRowHead(y, row)
}

// rowHandle paints the grip that follows lead on a movable row and records
// where it landed. The rail starts one column in, past its edge cell.
func (m *Model) rowHandle(entry treeRow, selected bool, lead string) string {
	if entry.isRoot() || m.renamingRow(entry) || m.mouseDisabled {
		return ""
	}
	if m.handleX == nil {
		m.handleX = map[string]int{}
	}
	m.handleX[rowKey(entry)] = 1 + ansi.StringWidth(lead)
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
	return m.reorder.active && rowKey(entry) == m.reorder.key
}

func (m *Model) enterReorder(key string) {
	m.lifts++
	m.menu = rowMenu{}
	m.errBar.text = ""
	m.reorder = reorderState{active: true, lift: m.lifts, key: key, dragging: true}
}

// exitReorder puts the lifted row down where it stands, or with keep false
// back where it was lifted from.
func (m *Model) exitReorder(keep bool) {
	for !keep && m.reorder.offset != 0 {
		back := -1
		if m.reorder.offset < 0 {
			back = 1
		}
		if !m.reorderStep(back) {
			break
		}
	}
	m.reorder = reorderState{}
}

// reorderStep moves the lifted row one visible sibling over, quietly
// refusing at either end of its level or once the row has left the list.
func (m *Model) reorderStep(delta int) bool {
	entry, ok := m.selectedRow()
	if !ok || rowKey(entry) != m.reorder.key {
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
	m.reorder.offset += delta
	m.reorder.moved = true
	return true
}

// stepLifted moves the lifted row from the keyboard or the wheel, which
// drops any pending move and hands the rail window back to the cursor.
func (m *Model) stepLifted(delta int) {
	m.reorder.drop = nil
	m.reorder.autoscroll.anchored = false
	m.reorderStep(delta)
}

// dragReorderTo steps the lifted row toward the row under the pointer,
// one sibling at a time, until the next sibling lies past the pointer.
func (m *Model) dragReorderTo(target int) {
	for m.cursor != target {
		entry, ok := m.selectedRow()
		if !ok {
			return
		}
		delta := 1
		if target < m.cursor {
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
	for i, row := range m.rows {
		if rowKey(row) == key {
			return i
		}
	}
	return -1
}

func (m *Model) handleReorderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := keybind.Normalize(msg.String())
	action, _ := m.listKeys.ActionFor(key)
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
		if !m.reorder.dragging {
			return m, nil
		}
		if row, ok := m.clickRow(msg.X, msg.Y); ok {
			m.dragTo(row)
		}
		return m, m.trackDragEdge(msg)
	case tea.MouseActionRelease:
		if !m.reorder.dragging {
			return m, nil
		}
		m.reorder.dragging = false
		switch {
		case m.reorder.drop != nil:
			m.commitDrop(*m.reorder.drop)
			m.exitReorder(true)
		case m.reorder.moved:
			m.exitReorder(true)
		default:
			m.reorder.autoscroll.anchored = false
		}
	case tea.MouseActionPress:
		row, onRail := m.clickRow(msg.X, msg.Y)
		if onRail && msg.Button == tea.MouseButtonLeft && rowKey(m.rows[row]) == m.reorder.key && m.onHandle(msg.X, msg.Y, row) {
			m.reorder.dragging = true
			return m, nil
		}
		m.exitReorder(true)
		return m.handleMouseEvent(msg)
	}
	return m, nil
}

func (m *Model) reorderFooter() string {
	if drop := m.reorder.drop; drop != nil {
		return m.transientFooter(legendSection{title: "Move", pairs: [][2]string{
			{"release", "move " + drop.label}, {"esc", "put back"},
		}})
	}
	return m.transientFooter(legendSection{title: "Reorder", pairs: [][2]string{
		{"drag / ↑↓ / wheel", "move"}, {"↵ / release", "drop"}, {"esc", "put back"},
	}})
}
