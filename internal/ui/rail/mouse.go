package rail

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	multiClickWindow = 400 * time.Millisecond
	autoscrollEvery  = 90 * time.Millisecond
)

func (m *Model) Hit(frame Frame, msg tea.MouseMsg, ctx MouseContext) (Selection, bool) {
	return frame.RowAt(msg.X, msg.Y-ctx.BodyOriginY, ctx.DividerX, ctx.FullWidth)
}

func (m *Model) Mouse(msg tea.MouseMsg, frame Frame, ctx MouseContext) Decision {
	if m.reorder.active {
		return m.reorderMouse(msg, frame, ctx)
	}
	if m.menu.active {
		return m.menuMouse(msg, frame, ctx)
	}
	if tea.MouseEvent(msg).IsWheel() {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.Move(-1, false)
		case tea.MouseButtonWheelDown:
			return m.Move(1, false)
		default:
			return Decision{Consumed: true}
		}
	}
	switch msg.Action {
	case tea.MouseActionPress:
		return m.mousePress(msg, frame, ctx)
	case tea.MouseActionMotion:
		if m.clickFocusKey != "" {
			selection, ok := m.Hit(frame, msg, ctx)
			if !ok || selection.key() != m.clickFocusKey {
				m.clickFocusKey = ""
			}
		}
		return Decision{Consumed: true}
	case tea.MouseActionRelease:
		return m.mouseRelease(msg, frame, ctx)
	default:
		return Decision{Consumed: true}
	}
}

func (m *Model) mousePress(msg tea.MouseMsg, frame Frame, ctx MouseContext) Decision {
	m.clickFocusKey = ""
	if msg.Button == tea.MouseButtonRight {
		selection, ok := m.Hit(frame, msg, ctx)
		if !ok || m.searching {
			return Decision{Consumed: true}
		}
		return m.openMenu(selection, msg.X, msg.Y, false)
	}
	if msg.Button != tea.MouseButtonLeft {
		return Decision{Consumed: true}
	}
	selection, ok := m.Hit(frame, msg, ctx)
	if !ok {
		return Decision{Consumed: true}
	}
	index := m.rowIndex(selection.key())
	if index < 0 {
		return Decision{Consumed: true}
	}
	previous := m.cursor
	if m.searching || !frame.InWindow(selection) {
		m.listClickAt = time.Time{}
		m.cursor = index
		return previewSelectionDecision(previous != index, selection)
	}
	now := ctx.Now
	if now.IsZero() {
		now = time.Now()
	}
	key := selection.key()
	double := !m.listClickAt.IsZero() && m.listClickKey == key && now.Sub(m.listClickAt) < multiClickWindow
	m.listClickAt, m.listClickKey = now, key
	localY := msg.Y - ctx.BodyOriginY
	if m.onHandle(msg.X, localY, selection, frame) {
		m.cursor = index
		m.listClickAt = time.Time{}
		m.enterReorder(key)
		return previewSelectionDecision(previous != index, selection)
	}
	if m.onMenuButton(msg.X, localY, selection, frame) {
		m.listClickAt = time.Time{}
		return m.openMenu(selection, msg.X, msg.Y, true)
	}
	m.cursor = index
	row := m.rows[index]
	decision := previewSelectionDecision(previous != index, selection)
	if !ctx.FullLayout && row.kind == SessionRow {
		if !row.sess.Archived {
			m.clickFocusKey = key
		}
		return decision
	}
	if !double {
		return decision
	}
	m.listClickAt = time.Time{}
	if row.kind == GroupRow {
		collapse := m.toggleCollapse()
		collapse.SelectionChanged = decision.SelectionChanged
		collapse.SelectionEffect = decision.SelectionEffect
		collapse.Selection = selection
		return collapse
	}
	if row.sess.Archived {
		return decision
	}
	decision.Intent = Intent{Kind: Focus, Target: selection}
	return decision
}

func (m *Model) mouseRelease(msg tea.MouseMsg, frame Frame, ctx MouseContext) Decision {
	key := m.clickFocusKey
	m.clickFocusKey = ""
	if key == "" || m.searching {
		return Decision{Consumed: true}
	}
	selection, ok := m.Hit(frame, msg, ctx)
	if !ok || selection.key() != key {
		return Decision{Consumed: true}
	}
	index := m.rowIndex(key)
	if index < 0 {
		return Decision{Consumed: true}
	}
	previous := m.cursor
	m.cursor = index
	decision := previewSelectionDecision(previous != index, selection)
	decision.Intent = Intent{Kind: Focus, Target: selection}
	return decision
}

func previewSelectionDecision(changed bool, selection Selection) Decision {
	decision := Decision{Consumed: true, SelectionChanged: changed, Selection: selection}
	if changed {
		decision.SelectionEffect = SelectionEffectPreview
	}
	return decision
}

func (m Model) onRowHead(localY int, selection Selection, frame Frame) bool {
	if !frame.InWindow(selection) || localY < 0 || localY >= len(frame.Lines) {
		return false
	}
	if localY == 0 {
		return true
	}
	previous := frame.Lines[localY-1]
	return !previous.HitOK || previous.Hit != selection
}

func (m Model) onHandle(x, localY int, selection Selection, frame Frame) bool {
	handle, ok := frame.Handles[selection.key()]
	return ok && x >= handle-1 && x <= handle+1 && m.onRowHead(localY, selection, frame)
}

func (m Model) onMenuButton(x, localY int, selection Selection, frame Frame) bool {
	return x >= frame.Width-menuButtonWidth && x <= frame.Width && m.onRowHead(localY, selection, frame)
}

func (m *Model) menuMouse(msg tea.MouseMsg, frame Frame, ctx MouseContext) Decision {
	if tea.MouseEvent(msg).IsWheel() {
		step := 1
		if msg.Button == tea.MouseButtonWheelUp {
			step = -1
		}
		m.menu.index = m.menu.nextItem(m.menu.index, step)
		return Decision{Consumed: true}
	}
	index, onItem := m.menuItemAt(msg.X, msg.Y, frame)
	switch msg.Action {
	case tea.MouseActionMotion:
		if onItem {
			m.menu.index = index
		}
		return Decision{Consumed: true}
	case tea.MouseActionRelease:
		held := m.menu.held
		m.menu.held = false
		if held && onItem {
			return m.runMenuItem(index)
		}
		return Decision{Consumed: true}
	}
	if onItem {
		if msg.Button == tea.MouseButtonLeft {
			return m.runMenuItem(index)
		}
		return Decision{Consumed: true}
	}
	m.menu = rowMenu{}
	if msg.Button == tea.MouseButtonRight {
		if selection, ok := m.Hit(frame, msg, ctx); ok {
			return m.openMenu(selection, msg.X, msg.Y, false)
		}
	}
	return Decision{Consumed: true}
}

func (m Model) menuItemAt(x, y int, frame Frame) (int, bool) {
	menu := frame.MenuRect
	if x <= menu.Left || x >= menu.Left+menu.Width-1 {
		return 0, false
	}
	index := y - menu.Top - 1
	return index, index >= 0 && index < len(m.menu.items) && m.menu.items[index].label != ""
}

func (m *Model) reorderMouse(msg tea.MouseMsg, frame Frame, ctx MouseContext) Decision {
	if tea.MouseEvent(msg).IsWheel() {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.stepLifted(-1)
		case tea.MouseButtonWheelDown:
			return m.stepLifted(1)
		}
		return Decision{Consumed: true}
	}
	switch msg.Action {
	case tea.MouseActionMotion:
		if !m.reorder.dragging {
			return Decision{Consumed: true}
		}
		decision := Decision{Consumed: true}
		if selection, ok := m.Hit(frame, msg, ctx); ok {
			if drop, resolved := m.resolveDrop(selection); resolved {
				m.reorder.drop = &drop
			} else {
				m.reorder.drop = nil
				decision.Mutations = m.planDrag(selection)
			}
		}
		decision.AutoScroll = m.trackDragEdge(msg, frame, ctx)
		return decision
	case tea.MouseActionRelease:
		if !m.reorder.dragging {
			return Decision{Consumed: true}
		}
		m.reorder.dragging = false
		switch {
		case m.reorder.drop != nil:
			request, ok := m.dropMutation(*m.reorder.drop)
			if !ok {
				m.finishReorder()
				return Decision{Consumed: true}
			}
			return Decision{Consumed: true, Mutations: []Mutation{request}}
		case m.reorder.moved:
			m.finishReorder()
		default:
			m.reorder.autoscroll.anchored = false
		}
		return Decision{Consumed: true}
	case tea.MouseActionPress:
		selection, onRail := m.Hit(frame, msg, ctx)
		if onRail && msg.Button == tea.MouseButtonLeft && selection.key() == m.reorder.key &&
			m.onHandle(msg.X, msg.Y-ctx.BodyOriginY, selection, frame) {
			m.reorder.dragging = true
			return Decision{Consumed: true}
		}
		m.finishReorder()
		return m.Mouse(msg, frame, ctx)
	default:
		return Decision{Consumed: true}
	}
}

func (m *Model) trackDragEdge(msg tea.MouseMsg, frame Frame, ctx MouseContext) *AutoScrollRequest {
	scroll := &m.reorder.autoscroll
	scroll.x, scroll.y = msg.X, msg.Y
	scroll.edge = m.dragEdge(msg.Y-ctx.BodyOriginY, frame)
	if scroll.edge == 0 || scroll.running {
		return nil
	}
	scroll.running = true
	return &AutoScrollRequest{Lift: m.reorder.lift, After: autoscrollEvery}
}

func (m Model) dragEdge(localY int, frame Frame) int {
	first, last := -1, -1
	for index, line := range frame.Lines {
		if line.HitOK {
			if first < 0 {
				first = index
			}
			last = index
		}
	}
	switch {
	case first >= 0 && localY <= first && frame.Window.Start > 0:
		return -1
	case last >= 0 && localY >= last && frame.Window.End < len(m.rows):
		return 1
	default:
		return 0
	}
}

func (m *Model) ApplyAutoScroll(tick AutoScrollTick, frame Frame, ctx MouseContext) Decision {
	if tick.Lift != m.reorder.lift {
		return Decision{Consumed: true}
	}
	scroll := &m.reorder.autoscroll
	if !m.reorder.dragging || scroll.edge == 0 {
		scroll.running = false
		return Decision{Consumed: true}
	}
	decision := Decision{Consumed: true}
	msg := tea.MouseMsg{X: scroll.x, Y: scroll.y}
	if selection, ok := m.Hit(frame, msg, ctx); ok {
		if drop, resolved := m.resolveDrop(selection); resolved {
			m.reorder.drop = &drop
		} else {
			m.reorder.drop = nil
			decision.Mutations = m.planDrag(selection)
		}
	}
	next := frame.Window.Start - 1
	if scroll.edge > 0 {
		next = frame.Window.End
	}
	if next < 0 || next >= len(m.rows) {
		scroll.running = false
		return decision
	}
	scroll.anchor, scroll.anchored = next, true
	scroll.edge = m.dragEdge(scroll.y-ctx.BodyOriginY, frame)
	decision.AutoScroll = &AutoScrollRequest{Lift: tick.Lift, After: autoscrollEvery}
	return decision
}
