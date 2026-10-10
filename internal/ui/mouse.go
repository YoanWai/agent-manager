package ui

import (
	"time"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// syncMouseCapture hands the mouse to the terminal while the setup dialog
// is up, so a drag over it selects the install command, and takes it back
// when the dialog closes. The mouse-mode setting does the same for anyone
// who wants native click-drag selection back everywhere else, except in
// focus mode: that pane's own mouse forwarding predates the setting and
// stays on regardless, the way it always has.
//
// An open row menu asks for every motion, pressed or not, so hovering an
// entry can light it up.
func (m *Model) syncMouseCapture() tea.Cmd {
	release := m.mode == modeLaunchHint || (m.prefs.mouseDisabled && m.mode != modeFocus)
	hover := !release && m.mode == modeList && m.rail.MenuOpen()
	if release == m.mouse.released && hover == m.mouse.hover {
		return nil
	}
	leavingHover := m.mouse.hover && !hover
	m.mouse.released, m.mouse.hover = release, hover
	switch {
	case release:
		return tea.DisableMouse
	case hover:
		return tea.EnableMouseAllMotion
	case leavingHover:
		return tea.Sequence(tea.DisableMouse, tea.EnableMouseCellMotion)
	default:
		return tea.EnableMouseCellMotion
	}
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action == tea.MouseActionPress {
		m.gens.foreground++
	}
	model, command := m.handleMouseEvent(msg)
	m.closeQuickOffTheList()
	return model, command
}

func (m *Model) closeQuickOffTheList() {
	if m.quick.active && m.mode != modeList {
		m.quick.active = false
		m.quick.release()
	}
}

func (m *Model) railMouseContext() uirail.MouseContext {
	return uirail.MouseContext{
		BodyOriginY: m.listChromeRows(),
		DividerX:    m.dividerX(),
		FullWidth:   m.fullRows(),
		FullLayout:  m.prefs.fullLayout,
		Now:         time.Now(),
	}
}

func (m *Model) handleMouseEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	ctx := m.railMouseContext()
	if m.mode == modeFocus {
		railPress := msg.Action == tea.MouseActionPress &&
			(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonRight)
		if railPress {
			if hit, ok := m.rail.Hit(m.layout.displayedRail, msg, ctx); ok {
				focused, _ := m.rail.Selected()
				left := m.leaveFocus()
				decision := m.rail.Mouse(msg, m.layout.displayedRail, ctx)
				if hit == focused {
					m.rail.CancelClickFocus()
				}
				model, command := m.applyRailDecision(decision)
				return model, tea.Batch(left, command)
			}
		}
		return m.handleFocusMouse(msg)
	}

	if m.mode == modeList && (m.rail.Reordering() || m.rail.MenuOpen()) {
		return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
	}
	if tea.MouseEvent(msg).IsWheel() {
		return m.handleMouseWheel(msg)
	}

	switch msg.Action {
	case tea.MouseActionPress:
		return m.handleMousePress(msg, ctx)
	case tea.MouseActionMotion:
		if m.layout.split.dragging {
			m.layout.split.moved = true
			m.setSplitFromX(msg.X)
			return m, nil
		}
		if m.mode == modeList {
			return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
		}
	case tea.MouseActionRelease:
		if m.layout.split.dragging {
			if !m.layout.split.moved {
				m.layout.split.dragging = false
				return m, nil
			}
			m.setSplitFromX(msg.X)
			return m.exitResizeMode(true)
		}
		if m.mode == modeList {
			return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
		}
	}
	return m, nil
}

func (m *Model) handleMousePress(msg tea.MouseMsg, ctx uirail.MouseContext) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight {
		return m, nil
	}
	if m.layout.split.dragging && !m.layout.split.resizeMode {
		m.exitResizeMode(m.layout.split.moved)
	}
	y0, y1 := m.bodyYRange()
	onDivider := msg.Button == tea.MouseButtonLeft && m.mode == modeList && !m.prefs.fullLayout && !m.rail.Searching() &&
		msg.Y >= y0 && msg.Y < y1 && m.onDivider(msg.X)
	if onDivider {
		m.layout.split.dragging = true
		m.layout.split.moved = false
		if !m.layout.split.resizeMode {
			m.layout.split.ratioBefore = m.layout.split.ratio
		}
		return m, nil
	}
	if m.mode == modeForm {
		return m.handleFormClick(msg.X, msg.Y)
	}
	if m.mode == modeHelp {
		if m.help.docsHit.contains(msg.X, msg.Y) {
			return m, openLink(docsURL)
		}
		return m, nil
	}
	if m.mode == modeSettings {
		return m.handleSettingsClick(msg.X, msg.Y)
	}
	if m.layout.split.resizeMode || m.mode != modeList {
		return m, nil
	}
	if m.notices.noticeHit.contains(msg.X, msg.Y) && !m.rail.Searching() {
		m.notices.open(m, "")
		return m, nil
	}
	if hit, ok := m.quick.hitAt(msg.X, msg.Y); ok {
		return m, m.handleQuickClick(hit)
	}
	return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
}

func (m *Model) handleMouseWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.layout.split.resizeMode || m.layout.split.dragging {
		return m, nil
	}
	switch m.mode {
	case modeList:
		return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, m.railMouseContext()))
	case modeDiff:
		state := m.review.Snapshot()
		if state.Annotating || state.SendConfirm {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.review.Wheel(-1, m.diffCodeHeight())
		case tea.MouseButtonWheelDown:
			m.review.Wheel(1, m.diffCodeHeight())
		}
	case modeNotices:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.notices.scroll(m, m.notices.active(m), -noticeWheelRows)
		case tea.MouseButtonWheelDown:
			m.notices.scroll(m, m.notices.active(m), noticeWheelRows)
		}
	}
	return m, nil
}
