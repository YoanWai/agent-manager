package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	splitRatioSetting = "split_ratio"
	defaultSplitRatio = 0.3
	minSplitSide      = 30
	// How many columns on either side of the panel junction count as the
	// divider hit target. Wide enough to grab without hunting.
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
func (m *Model) listChromeRows() int { return m.headerRows() + 1 }

// listBodyHeight is the vertical budget for the sessions/sidebar panels.
// Matches View: height - (header, seam, status when one shows, footer).
func (m *Model) listBodyHeight() int {
	statusRow := 0
	if m.viewStatus() != "" {
		statusRow = 1
	}
	bodyHeight := m.height - m.listChromeRows() - 1 - statusRow - lipgloss.Height(m.viewFooter())
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
// split junction to start a drag.
func (m *Model) onDivider(x int) bool {
	div := m.dividerX()
	return x >= div-splitHitSlop && x <= div+splitHitSlop
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Focus mode owns the mouse: clicks build a selection over the pane
	// instead of moving the list cursor, which would silently retarget
	// every following keystroke.
	if m.mode == modeFocus {
		return m.handleFocusMouse(msg)
	}

	// Mouse events are always consumed so the host terminal / outer tmux
	// never scrolls the manager off-screen. Wheel maps to in-app
	// navigation; clicks only drive the divider while resize mode is armed.
	if tea.MouseEvent(msg).IsWheel() {
		return m.handleMouseWheel(msg)
	}
	if !m.split.resizeMode {
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		y0, y1 := m.bodyYRange()
		if msg.Y < y0 || msg.Y >= y1 {
			return m, nil
		}
		if !m.onDivider(msg.X) {
			return m, nil
		}
		m.split.dragging = true
		m.split.ratioBefore = m.split.ratio
		m.setSplitFromX(msg.X)
		return m, nil

	case tea.MouseActionMotion:
		if !m.split.dragging {
			return m, nil
		}
		// Button may be reported as left or none depending on terminal;
		// once a drag has started, any motion updates the live ratio.
		m.setSplitFromX(msg.X)
		return m, nil

	case tea.MouseActionRelease:
		if !m.split.dragging {
			return m, nil
		}
		m.setSplitFromX(msg.X)
		return m.exitResizeMode(true)
	}
	return m, nil
}

// handleMouseWheel keeps the wheel inside the app so the outer terminal
// cannot scroll the manager away. It scrolls the diff, and is swallowed
// everywhere else: in the list a notch would move the session cursor,
// silently retargeting every keystroke that follows (#110).
func (m *Model) handleMouseWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.split.resizeMode || m.mode != modeDiff {
		return m, nil
	}
	if m.diff.annotating || m.diff.sendConfirm {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.moveDiffCursor(-1, m.diffCodeHeight())
	case tea.MouseButtonWheelDown:
		m.moveDiffCursor(1, m.diffCodeHeight())
	}
	return m, nil
}
