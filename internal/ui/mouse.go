package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

// multiClickWindow also governs rail double-clicks; Focus keeps its own
// private copy for pane selection runs.
const multiClickWindow = 400 * time.Millisecond

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
	hover := !release && m.mode == modeList && m.rail.menu.active
	if release == m.mouseReleased && hover == m.mouseHover {
		return nil
	}
	leavingHover := m.mouseHover && !hover
	m.mouseReleased, m.mouseHover = release, hover
	switch {
	case release:
		return tea.DisableMouse
	case hover:
		return tea.EnableMouseAllMotion
	case leavingHover:
		// Any-motion tracking is a private mode of its own, and button
		// tracking does not reset it: DisableMouse does, before button
		// tracking comes back.
		return tea.Sequence(tea.DisableMouse, tea.EnableMouseCellMotion)
	}
	return tea.EnableMouseCellMotion
}

// handleMouse also closes the quick bar when a click took the app out of
// the list: the bar and its legend belong to the list alone.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	model, cmd := m.handleMouseEvent(msg)
	m.closeQuickOffTheList()
	return model, cmd
}

func (m *Model) closeQuickOffTheList() {
	if m.quick.active && m.mode != modeList {
		m.quick.active = false
		m.quick.release()
	}
}

func (m *Model) handleMouseEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeFocus {
		railPress := msg.Action == tea.MouseActionPress &&
			(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonRight)
		if railPress {
			// A press on a rail row leaves focus so the same click can
			// act on that row. Only a rail row: the pane's own blank tail,
			// the chrome, and a full screen session all keep the keyboard.
			if row, onRail := m.clickRow(msg.X, msg.Y); onRail {
				focused := m.rail.cursor
				left := m.leaveFocus()
				model, cmd := m.handleMousePress(msg)
				// The focused session's own row is the way back: its
				// release must not focus it again.
				if row == focused {
					m.rail.clickFocusKey = ""
				}
				return model, tea.Batch(left, cmd)
			}
		}
		return m.handleFocusMouse(msg)
	}
	if m.mode == modeList && m.rail.reorder.active {
		return m.handleReorderMouse(msg)
	}
	if m.mode == modeList && m.rail.menu.active {
		return m.handleMenuMouse(msg)
	}

	// Mouse events are always consumed so the host terminal / outer tmux
	// never scrolls the manager off-screen.
	if tea.MouseEvent(msg).IsWheel() {
		return m.handleMouseWheel(msg)
	}

	switch msg.Action {
	case tea.MouseActionPress:
		return m.handleMousePress(msg)

	case tea.MouseActionMotion:
		if m.rail.clickFocusKey != "" && m.rowKeyAt(msg.X, msg.Y) != m.rail.clickFocusKey {
			m.rail.clickFocusKey = ""
		}
		if !m.split.dragging {
			return m, nil
		}
		// Button may be reported as left or none depending on terminal;
		// once a drag has started, any motion updates the live ratio.
		m.split.moved = true
		m.setSplitFromX(msg.X)
		return m, nil

	case tea.MouseActionRelease:
		if m.rail.clickFocusKey != "" {
			return m.releaseClickFocus(msg)
		}
		if !m.split.dragging {
			return m, nil
		}
		if !m.split.moved {
			// A press and release on the seam with no motion between them
			// is a click, not a resize: committing would persist a ratio
			// nobody dragged to and reflow every live pane for a frame
			// that never changed.
			m.split.dragging = false
			return m, nil
		}
		m.setSplitFromX(msg.X)
		return m.exitResizeMode(true)
	}
	return m, nil
}

// handleMousePress resolves a left press against the divider first, then a
// session row: dragging the seam has to win over the row beside it. Neither
// needs resize mode armed from the keyboard first. A press while resize
// mode is already armed but off the divider is left alone, waiting, exactly
// as it did before the mouse could arm it too.
func (m *Model) handleMousePress(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonRight {
		return m.handleRightPress(msg)
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	m.rail.clickFocusKey = ""
	// A drag whose release never landed is still holding dragging, and the
	// release of this press would be read as its own: the divider would
	// jump to wherever this click is and persist there. End the stale one
	// first, the way the next key does in handleKey.
	if m.split.dragging && !m.split.resizeMode {
		m.exitResizeMode(m.split.moved)
	}
	y0, y1 := m.bodyYRange()
	// Full layout paints no divider; splitWidths still returns a ratio-based
	// column, so without this check a click there would arm a drag instead
	// of landing on the rail row it's actually over. The search field
	// holds the divider too, as it does for the keyboard in enterResizeMode.
	onDivider := m.mode == modeList && !m.prefs.fullLayout && !m.rail.searching &&
		msg.Y >= y0 && msg.Y < y1 && m.onDivider(msg.X)
	if onDivider {
		// A mouse drag arms dragging alone. resizeMode is the keyboard's
		// gate and takes the keyboard with it, which would strand the list
		// on a press whose release never lands, dragging out of the window.
		m.split.dragging = true
		m.split.moved = false
		if !m.split.resizeMode {
			m.split.ratioBefore = m.split.ratio
		}
		return m, nil
	}
	if m.split.resizeMode || m.mode != modeList {
		return m, nil
	}
	if m.notices.noticeHit.contains(msg.X, msg.Y) && !m.rail.searching {
		m.openNotices("")
		return m, nil
	}
	if row, ok := m.clickRow(msg.X, msg.Y); ok {
		// Search owns Enter, and a "more" counter only steps the window onto
		// the row it hides, so neither opens a click run: one left standing
		// would pair with the next press.
		if m.rail.searching || !m.inRailWindow(row) {
			m.rail.listClickAt = time.Time{}
			return m, m.selectRow(row)
		}
		// Matched on the row's identity: the poll rebuilds m.rows between
		// the presses, so one index can name two different rows.
		key := rowKey(m.rail.rows[row])
		double := !m.rail.listClickAt.IsZero() && m.rail.listClickKey == key && time.Since(m.rail.listClickAt) < multiClickWindow
		m.rail.listClickAt, m.rail.listClickKey = time.Now(), key
		if m.onHandle(msg.X, msg.Y, row) {
			cmd := m.selectRow(row)
			m.rail.listClickAt = time.Time{}
			m.enterReorder(key)
			return m, cmd
		}
		if m.onMenuButton(msg.X, msg.Y, row) {
			m.rail.listClickAt = time.Time{}
			cmd := m.openRowMenu(row, msg.X, msg.Y)
			m.rail.menu.held = true
			return m, cmd
		}
		// Both gestures act on the row under the pointer, so the cursor
		// goes there first: a wheel notch or a key between the presses
		// leaves it somewhere else.
		cmd := m.selectRow(row)
		entry := m.rail.rows[row]
		if !m.prefs.fullLayout && !entry.isGroup {
			if !entry.sess.Archived {
				m.rail.clickFocusKey = key
			}
			return m, cmd
		}
		if !double {
			return m, cmd
		}
		m.rail.listClickAt = time.Time{} // consume the pair so a third press starts a new run
		if entry.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		if entry.sess.Archived {
			return m, cmd
		}
		// Focus owns m.preview from here, so the preview that select
		// scheduled is dropped rather than left to land on it.
		return m.focusSelected()
	}
	return m, nil
}

// handleRightPress opens the row menu on the rail row under the pointer.
func (m *Model) handleRightPress(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeList || m.split.resizeMode || m.split.dragging || m.rail.searching {
		return m, nil
	}
	if row, ok := m.clickRow(msg.X, msg.Y); ok {
		return m, m.openRowMenu(row, msg.X, msg.Y)
	}
	return m, nil
}

// releaseClickFocus focuses the session a split rail press armed, provided
// the release lands back on that row.
func (m *Model) releaseClickFocus(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	key := m.rail.clickFocusKey
	m.rail.clickFocusKey = ""
	if m.mode != modeList || m.rail.searching || m.rowKeyAt(msg.X, msg.Y) != key {
		return m, nil
	}
	row, _ := m.clickRow(msg.X, msg.Y)
	// Focus owns m.preview, so a preview this select schedules is dropped.
	m.selectRow(row)
	return m.focusSelected()
}

// rowKeyAt names the rail row under (x, y), or "" off the rows.
func (m *Model) rowKeyAt(x, y int) string {
	row, ok := m.clickRow(x, y)
	if !ok {
		return ""
	}
	return rowKey(m.rail.rows[row])
}

// inRailWindow reports whether row is painted in the rail's window. A
// "more" counter answers for a row outside it.
func (m *Model) inRailWindow(row int) bool {
	return row >= m.rail.railTop && row < m.rail.railEnd
}

// onRowHead reports whether y is the first line row painted, where its
// handle and menu button sit.
func (m *Model) onRowHead(y, row int) bool {
	if !m.inRailWindow(row) {
		return false
	}
	y0, _ := m.bodyYRange()
	line := y - y0
	return line == 0 || m.rail.railHits[line-1] != row
}

// clickRow reports which m.rows index a press at (x, y) selects, reading
// the geometry recordRailHits took off the frame the rail painted rather
// than re-deriving column and row offsets here, where they would drift the
// first time the layout moves (#110).
func (m *Model) clickRow(x, y int) (int, bool) {
	if !m.fullRows() && x >= m.dividerX() {
		return 0, false
	}
	y0, y1 := m.bodyYRange()
	if y < y0 || y >= y1 {
		return 0, false
	}
	idx := y - y0
	if idx >= len(m.rail.railHits) {
		return 0, false
	}
	row := m.rail.railHits[idx]
	// A rebuild between the paint and the press can shrink m.rows under
	// the hits this frame recorded.
	if row < 0 || row >= len(m.rail.rows) {
		return 0, false
	}
	return row, true
}

// handleMouseWheel keeps the wheel inside the app so the outer terminal
// cannot scroll the manager away: it moves the session cursor in the list,
// same as an arrow key, and the diff cursor in review. The search field
// and the quick bar keep it: search otherwise leaves a narrowed list with
// no way to reach a row in it, and the quick bar retargets on up/down the
// way its own footer advertises.
func (m *Model) handleMouseWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.split.resizeMode || m.split.dragging {
		return m, nil
	}
	switch m.mode {
	case modeList:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m, m.scrollCursor(-1)
		case tea.MouseButtonWheelDown:
			return m, m.scrollCursor(1)
		}
	case modeDiff:
		if m.diff.annotating || m.diff.sendConfirm {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.moveDiffCursor(-1, m.diffCodeHeight())
		case tea.MouseButtonWheelDown:
			m.moveDiffCursor(1, m.diffCodeHeight())
		}
	}
	return m, nil
}
