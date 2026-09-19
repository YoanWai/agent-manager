package ui

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	splitRatioSetting = "split_ratio"
	defaultSplitRatio = 0.3
	minSplitSide      = 30
	// How many columns past the panel junction count as the divider hit
	// target, on top of the junction itself. Wide enough to grab without
	// hunting, and one-sided on purpose: see onDivider.
	splitHitSlop = 1
)

// settingReader is the store surface loadSplitRatio needs; tests stub it.
type settingReader interface {
	Setting(key string) (string, error)
}

// loadSplitRatio restores the sessions/sidebar ratio, or the 30% default
// when nothing is stored or the value is unusable.
func loadSplitRatio(st settingReader) float64 {
	raw, err := st.Setting(splitRatioSetting)
	if err != nil || raw == "" {
		return defaultSplitRatio
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio <= 0 || ratio >= 1 {
		return defaultSplitRatio
	}
	return ratio
}

// persistSplitRatio writes the current ratio so the next launch reopens
// at the same split.
func (m *Model) persistSplitRatio() {
	if m.store == nil {
		return
	}
	if err := m.store.SetSetting(splitRatioSetting, strconv.FormatFloat(m.split.ratio, 'f', 4, 64)); err != nil {
		m.errBar.text = err.Error()
	}
}

// clampSplitLeft keeps both panels above minSplitSide when the terminal
// is wide enough; on a narrow terminal it just keeps both sides visible.
func clampSplitLeft(left, width int) int {
	if width < 2 {
		if width < 1 {
			return 0
		}
		return 1
	}
	if width < minSplitSide*2 {
		if left < 1 {
			left = 1
		}
		if left >= width {
			left = width - 1
		}
		return left
	}
	if left < minSplitSide {
		left = minSplitSide
	}
	if width-left < minSplitSide {
		left = width - minSplitSide
	}
	return left
}

// setSplitFromX pins the left panel's right edge to terminal column x and
// updates the stored ratio. Live during a drag; consumers re-read via
// splitWidths on the next View.
func (m *Model) setSplitFromX(x int) {
	if m.width <= 0 {
		return
	}
	left := clampSplitLeft(x, m.width)
	m.split.ratio = float64(left) / float64(m.width)
}

// enterResizeMode arms divider dragging, which the arrow keys drive. The
// app holds mouse reporting, so a drag here reaches handleMouse too.
func (m *Model) enterResizeMode() (tea.Model, tea.Cmd) {
	if m.mode != modeList || m.searching || m.quick.active {
		return m, nil
	}
	m.split.resizeMode = true
	m.split.dragging = false
	m.split.ratioBefore = m.split.ratio
	m.errBar.text = ""
	return m, nil
}

// exitResizeMode leaves divider-drag mode. When commit is true the current
// ratio is persisted; cancel restores the pre-mode ratio. Either path ends
// with a pane resize so the preview stays 1:1 with the panel.
func (m *Model) exitResizeMode(commit bool) (tea.Model, tea.Cmd) {
	if !m.split.resizeMode && !m.split.dragging {
		return m, nil
	}
	if !commit {
		m.split.ratio = m.split.ratioBefore
	} else {
		m.persistSplitRatio()
	}
	m.split.dragging = false
	m.split.moved = false
	m.split.resizeMode = false
	m.resizeSessions()
	return m, nil
}

// nudgeSplit moves the divider by delta columns while resize mode is on.
// UI reflows instantly; tmux reflow is deferred to commit (| / mouse up)
// so holding an arrow does not spawn a resize-window per keystroke.
func (m *Model) nudgeSplit(delta int) {
	if m.width <= 0 || delta == 0 {
		return
	}
	left, _ := m.splitWidths()
	m.setSplitFromX(left + delta)
}

// listChromeRows is the number of rows above the sessions/content body:
// the full-width header band and the rule that closes it. Shared by View
// and bodyYRange so hit-testing cannot drift from paint.
func (m *Model) listChromeRows() int {
	// A session open full screen names itself on a line of its own, held
	// off the band above it and the pane below it by a rule each.
	if m.fullFocus() {
		return m.headerRows() + 3
	}
	return m.headerRows() + 1
}

// listBodyHeight is the vertical budget for the sessions/sidebar panels.
// Matches View: height - (header, seam, footer). Notices float over the body
// instead of taking a row, so the budget is the same with one up.
func (m *Model) listBodyHeight() int {
	bodyHeight := m.height - m.listChromeRows() - 1 - lipgloss.Height(m.viewFooter())
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	return bodyHeight
}

// bodyYRange is the inclusive-start exclusive-end row range of the main
// sessions/sidebar body, matching the layout in View.
func (m *Model) bodyYRange() (start, end int) {
	return m.listChromeRows(), m.listChromeRows() + m.listBodyHeight()
}

// dividerX is the column index of the sessions/sidebar junction (first
// column of the right panel, or the grip column when resize mode is on).
func (m *Model) dividerX() int {
	left, _ := m.splitWidths()
	return left
}

// onDivider reports whether terminal column x is close enough to the
// split junction to start a drag. The slop reaches right only: the seam
// and the bleed beside it carry no row text, while dividerX-1 is the
// rail's own last painted column. Grabbing that back would cost every row
// its right edge, since the divider is resolved before the row is (#110).
func (m *Model) onDivider(x int) bool {
	div := m.dividerX()
	return x >= div && x <= div+splitHitSlop
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeFocus {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// A press on a rail row leaves focus so the same click can
			// select that row. Only a rail row: the pane's own blank tail,
			// the chrome, and a full screen session all keep the keyboard.
			if _, onRail := m.clickRow(msg.X, msg.Y); onRail {
				left := m.leaveFocus()
				model, cmd := m.handleMousePress(msg)
				return model, tea.Batch(left, cmd)
			}
		}
		return m.handleFocusMouse(msg)
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
		if !m.split.dragging {
			return m, nil
		}
		// Button may be reported as left or none depending on terminal;
		// once a drag has started, any motion updates the live ratio.
		m.split.moved = true
		m.setSplitFromX(msg.X)
		return m, nil

	case tea.MouseActionRelease:
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
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
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
	// of landing on the rail row it's actually over. The search field and
	// the quick bar hold the divider too, so the mouse arms it under the
	// same conditions the keyboard does in enterResizeMode.
	onDivider := m.mode == modeList && !m.fullLayout && !m.searching && !m.quick.active &&
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
	if m.noticeHit.contains(msg.X, msg.Y) && m.listReadyForNotice() {
		m.openNotices("")
		return m, nil
	}
	if row, ok := m.clickRow(msg.X, msg.Y); ok {
		// Search and the quick bar own Enter, so a press there selects and
		// opens no run: one left standing would pair with the first press
		// after the surface closes.
		if m.searching || m.quick.active {
			m.listClickAt = time.Time{}
			return m, m.selectRow(row)
		}
		// Matched on the row's identity: the poll rebuilds m.rows between
		// the presses, so one index can name two different rows.
		key := rowKey(m.rows[row])
		double := !m.listClickAt.IsZero() && m.listClickKey == key && time.Since(m.listClickAt) < multiClickWindow
		m.listClickAt, m.listClickKey = time.Now(), key
		// Both gestures act on the row under the pointer, so the cursor
		// goes there first: a wheel notch or a key between the presses
		// leaves it somewhere else.
		cmd := m.selectRow(row)
		if !double {
			return m, cmd
		}
		m.listClickAt = time.Time{} // consume the pair so a third press starts a new run
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		// Focus owns m.preview from here, so the preview that select
		// scheduled is dropped rather than left to land on it.
		return m.focusSelected()
	}
	return m, nil
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
	if idx >= len(m.railHits) {
		return 0, false
	}
	row := m.railHits[idx]
	// A rebuild between the paint and the press can shrink m.rows under
	// the hits this frame recorded.
	if row < 0 || row >= len(m.rows) {
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
