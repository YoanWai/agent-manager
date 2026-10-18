package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// syncMouseCapture hands the mouse to the terminal for anyone who turned
// the mouse-mode setting off and wants native click-drag selection back,
// except in focus mode: that pane's own mouse forwarding predates the
// setting and stays on regardless, the way it always has.
//
// An open row menu asks for every motion, pressed or not, so hovering an
// entry can light it up.
func (m *Model) syncMouseCapture() tea.Cmd {
	release := m.prefs.mouseDisabled && m.mode != modeFocus
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
	model, command := m.routeMouse(msg)
	m.closeQuickOffTheList()
	return model, command
}

func (m *Model) closeQuickOffTheList() {
	if m.quick.active && m.mode != modeList {
		m.quick.active = false
		m.quick.release()
	}
}

// routeMouse hands an event to the surface it belongs to: a card floating
// over the frame, the focused pane, Review, or the list. A painted key runs
// as if typed wherever it shows. A rename typed into its row holds the
// list still.
func (m *Model) routeMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if key, ok := m.paintedKeyAt(msg); ok {
		return m.handleKey(key)
	}
	switch {
	case m.cardOpen():
		return m.cardMouse(msg)
	case m.mode == modeFocus:
		return m.focusMouse(msg)
	case m.mode == modeDiff:
		return m.reviewMouse(msg)
	case m.mode == modeRename:
		return m, nil
	}
	return m.listMouse(msg)
}

// paintedKeyAt is the painted key pair a left press landed on, which the
// press runs as if typed. The search field and the row menu keep the press:
// typing owns the keys the list footer names, and the menu floats above it.
func (m *Model) paintedKeyAt(msg tea.MouseMsg) (tea.KeyMsg, bool) {
	if !isLeftPress(msg) {
		return tea.KeyMsg{}, false
	}
	if m.mode == modeList && (m.rail.Searching() || m.rail.MenuOpen()) {
		return tea.KeyMsg{}, false
	}
	return m.layout.keyHits.at(msg.X, msg.Y)
}

func isLeftPress(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
}

// wheelArrow is the arrow key a wheel notch stands for, in the lists that
// take the wheel the way they take ↑↓.
func wheelArrow(msg tea.MouseMsg) (tea.KeyMsg, bool) {
	switch presentation.WheelStep(msg) {
	case -1:
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case 1:
		return tea.KeyMsg{Type: tea.KeyDown}, true
	}
	return tea.KeyMsg{}, false
}

// cardOpen reports a card floating over the frame, which takes the mouse.
func (m *Model) cardOpen() bool {
	switch m.mode {
	case modeList, modeDiff, modeRename:
		return false
	case modeFocus:
		return m.focus.saved.list.Open()
	}
	return true
}

// cardMouse runs the wheel and a left press on the open card. The wheel
// turns the card under the pointer and nothing off it. A press on the
// body acts on what the body painted there, and one off the card closes
// it the way esc does.
func (m *Model) cardMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	card := m.layout.card
	if tea.MouseEvent(msg).IsWheel() {
		if !card.holds(msg.X, msg.Y) {
			return m, nil
		}
		return m.wheelCard(msg)
	}
	if !isLeftPress(msg) {
		return m, nil
	}
	if !card.holds(msg.X, msg.Y) {
		if m.closesOnBackdrop() {
			return m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
		}
		return m, nil
	}
	if col, line, ok := card.bodyAt(msg.X, msg.Y); ok {
		return m.clickCard(col, line)
	}
	return m, nil
}

// closesOnBackdrop reports whether a click off the open card closes it the
// way esc does. A card holding typed input keeps its draft through a stray
// click.
func (m *Model) closesOnBackdrop() bool {
	switch m.mode {
	case modeHelp, modeNotices, modeMove, modeRepoPick, modeConfirmDelete, modeLaunchHint:
		return true
	case modeSettings:
		return !m.settings.dialog.Editor.Typing() && !m.settings.dialog.Prompts.Editing()
	}
	return false
}

// clickCard acts on the open card's body at col of its painted line.
func (m *Model) clickCard(col, line int) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.handleFormClick(line)
	case modeGroupForm:
		return m.handleGroupFormClick(line)
	case modeSettings:
		return m.applySettingsResult(m.settings.dialog.Click(col, line, m.settingsContext()))
	case modeHelp:
		m.help.state.Click(line)
	case modeNotices:
		return m.handleNoticesClick(line)
	case modeMove:
		return m.handleMoveClick(line)
	case modeRepoPick:
		return m, m.handleRepoPickClick(line)
	case modeLaunchHint:
		return m, m.launchHint.handleClick(m, line)
	case modeFocus:
		return m, m.handleFocusSavedClick(line)
	}
	return m, nil
}

// wheelCard steps the open card's list the way ↑↓ do, or scrolls the card
// that reads.
func (m *Model) wheelCard(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	arrow, ok := wheelArrow(msg)
	if !ok {
		return m, nil
	}
	step := presentation.WheelStep(msg)
	switch m.mode {
	case modeForm:
		return m, m.runFormRequest(m.form.Wheel(m.spawnHost(), step), nil)
	case modeGroupForm:
		return m, m.runGroupFormKey(m.groupForm.Wheel(step), nil)
	case modeSettings:
		return m.applySettingsResult(m.settings.dialog.Wheel(step, m.settingsContext()))
	case modeNotices:
		m.notices.Wheel(step, m.activeNotices(), m.noticesFrame())
	case modeHelp:
		return m.handleHelpKey(arrow)
	case modeMove:
		return m.handleMoveKey(arrow)
	case modeRepoPick:
		return m, m.handleRepoPickKey(arrow)
	case modeFocus:
		cmd, _ := m.focus.saved.list.Key(arrow)
		return m, cmd
	}
	return m, nil
}

// focusMouse runs the mouse on a focused session. A press on a rail row
// leaves focus for that row, and a left press on the rail's blank leaves;
// everything else is the pane's.
func (m *Model) focusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	railPress := msg.Action == tea.MouseActionPress &&
		(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonRight)
	if !railPress {
		return m.handleFocusMouse(msg)
	}
	ctx := m.railMouseContext()
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
	if msg.Button == tea.MouseButtonLeft && m.onRailColumn(msg.X, msg.Y) {
		return m, m.leaveFocus()
	}
	return m.handleFocusMouse(msg)
}

// reviewMouse runs the wheel and a left press on Review's panes.
func (m *Model) reviewMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if tea.MouseEvent(msg).IsWheel() {
		m.review.Wheel(presentation.WheelStep(msg), m.diffCodeHeight())
		return m, nil
	}
	if isLeftPress(msg) {
		return m, m.handleDiffClick(msg.X, msg.Y)
	}
	return m, nil
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

// listMouse runs the mouse on the list screen: the rail, the split's
// divider, the preview beside the rail, and the quick bar.
func (m *Model) listMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	ctx := m.railMouseContext()
	if m.rail.Reordering() || m.rail.MenuOpen() {
		return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
	}
	if tea.MouseEvent(msg).IsWheel() {
		return m.listWheel(msg, ctx)
	}
	split := &m.layout.split
	switch msg.Action {
	case tea.MouseActionPress:
		return m.listPress(msg, ctx)
	case tea.MouseActionMotion:
		if split.dragging {
			split.moved = true
			m.setSplitFromX(msg.X)
			return m, nil
		}
	case tea.MouseActionRelease:
		if split.dragging {
			if !split.moved {
				split.dragging = false
				return m, nil
			}
			m.setSplitFromX(msg.X)
			return m.exitResizeMode(true)
		}
	}
	return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
}

// listWheel scrolls the rail, or the list open over the quick bar's
// prompt, which owns the wheel the way it owns ↑↓. A resize keeps both
// still.
func (m *Model) listWheel(msg tea.MouseMsg, ctx uirail.MouseContext) (tea.Model, tea.Cmd) {
	if m.layout.split.resizeMode || m.layout.split.dragging {
		return m, nil
	}
	if !m.quickPicking() {
		return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
	}
	if arrow, ok := wheelArrow(msg); ok {
		return m.handleQuickKey(arrow)
	}
	return m, nil
}

func (m *Model) listPress(msg tea.MouseMsg, ctx uirail.MouseContext) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight {
		return m, nil
	}
	split := &m.layout.split
	if split.dragging && !split.resizeMode {
		m.exitResizeMode(split.moved)
	}
	y0, y1 := m.bodyYRange()
	onDivider := msg.Button == tea.MouseButtonLeft && !m.prefs.fullLayout && !m.rail.Searching() &&
		msg.Y >= y0 && msg.Y < y1 && m.onDivider(msg.X)
	if onDivider {
		split.dragging, split.moved = true, false
		if !split.resizeMode {
			split.ratioBefore = split.ratio
		}
		return m, nil
	}
	if split.resizeMode {
		return m, nil
	}
	if hit, ok := m.quick.hitAt(msg.X, msg.Y); ok {
		return m, m.handleQuickClick(hit)
	}
	if msg.Button == tea.MouseButtonLeft && m.onContentColumn(msg.X, msg.Y) {
		return m.focusFromPreview()
	}
	return m.applyRailDecision(m.rail.Mouse(msg, m.layout.displayedRail, ctx))
}

// onRailColumn and onContentColumn place a point in the split's body on
// either side of the divider, which belongs to neither.
func (m *Model) onRailColumn(x, y int) bool {
	y0, y1 := m.bodyYRange()
	return !m.prefs.fullLayout && x < m.dividerX() && y >= y0 && y < y1
}

func (m *Model) onContentColumn(x, y int) bool {
	y0, y1 := m.bodyYRange()
	return !m.prefs.fullLayout && x > m.dividerX()+splitHitSlop && y >= y0 && y < y1
}

// focusFromPreview focuses the session under the cursor from a press on the
// preview beside it. The press only focuses: it never reaches the agent.
// A group, an archived session, the search field and the quick bar keep it.
func (m *Model) focusFromPreview() (tea.Model, tea.Cmd) {
	row, ok := m.selectedRow()
	if !ok || row.isGroup || row.sess.Archived || m.rail.Searching() || m.quick.active {
		return m, nil
	}
	return m.focusSelected()
}
