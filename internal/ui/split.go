package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	splitRatioSetting = "split_ratio"
	defaultSplitRatio = 0.34
	minSplitSide      = 30
	// How many columns on either side of the panel junction count as the
	// divider hit target. Wide enough to grab without hunting.
	splitHitSlop = 1
)

// settingReader is the store surface loadSplitRatio needs; tests stub it.
type settingReader interface {
	Setting(key string) (string, error)
}

// loadSplitRatio restores the sessions/sidebar ratio, or the 34% default
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
	if err := m.store.SetSetting(splitRatioSetting, strconv.FormatFloat(m.splitRatio, 'f', 4, 64)); err != nil {
		m.err = err.Error()
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
	m.splitRatio = float64(left) / float64(m.width)
}

// enterResizeMode arms divider dragging. Mouse reporting is always on at
// the program level (see main) so the terminal cannot scroll the TUI away;
// this mode only changes how clicks/drags are interpreted.
func (m *Model) enterResizeMode() (tea.Model, tea.Cmd) {
	if m.mode != modeList || m.searching || m.quick.active {
		return m, nil
	}
	m.resizeMode = true
	m.splitDragging = false
	m.splitRatioBefore = m.splitRatio
	m.err = ""
	return m, nil
}

// exitResizeMode leaves divider-drag mode. When commit is true the current
// ratio is persisted; cancel restores the pre-mode ratio. Either path ends
// with a pane resize so the preview stays 1:1 with the panel.
func (m *Model) exitResizeMode(commit bool) (tea.Model, tea.Cmd) {
	if !m.resizeMode && !m.splitDragging {
		return m, nil
	}
	if !commit {
		m.splitRatio = m.splitRatioBefore
	} else {
		m.persistSplitRatio()
	}
	m.splitDragging = false
	m.resizeMode = false
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

// listChromeRows is the number of rows above the sessions/sidebar body
// in list mode: header + blank separator. Shared by View and bodyYRange
// so hit-testing cannot drift from paint.
const listChromeRows = 2

// listBodyHeight is the vertical budget for the sessions/sidebar panels.
// Matches View: height - (header, blank, status, footer baseline).
func (m *Model) listBodyHeight() int {
	bodyHeight := m.height - 4 - lipgloss.Height(m.viewFooter())
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	return bodyHeight
}

// bodyYRange is the inclusive-start exclusive-end row range of the main
// sessions/sidebar body, matching the layout in View.
func (m *Model) bodyYRange() (start, end int) {
	return listChromeRows, listChromeRows + m.listBodyHeight()
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
	// Always consume mouse events so the host terminal / outer tmux never
	// scrolls the manager off-screen. Wheel maps to in-app navigation;
	// clicks only drive the divider while resize mode is armed.
	if tea.MouseEvent(msg).IsWheel() {
		return m.handleMouseWheel(msg)
	}
	if !m.resizeMode {
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
		m.splitDragging = true
		m.splitRatioBefore = m.splitRatio
		m.setSplitFromX(msg.X)
		return m, nil

	case tea.MouseActionMotion:
		if !m.splitDragging {
			return m, nil
		}
		// Button may be reported as left or none depending on terminal;
		// once a drag has started, any motion updates the live ratio.
		m.setSplitFromX(msg.X)
		return m, nil

	case tea.MouseActionRelease:
		if !m.splitDragging {
			return m, nil
		}
		m.setSplitFromX(msg.X)
		return m.exitResizeMode(true)
	}
	return m, nil
}

// handleMouseWheel keeps the wheel inside the app: list cursor, diff
// scroll, or a no-op swallow so the outer terminal cannot scroll away.
func (m *Model) handleMouseWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.resizeMode {
		return m, nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -1
	case tea.MouseButtonWheelDown:
		delta = 1
	default:
		return m, nil
	}
	switch m.mode {
	case modeDiff:
		if m.diff.annotating || m.diff.sendConfirm {
			return m, nil
		}
		m.moveDiffCursor(delta, m.diffCodeHeight())
		return m, nil
	case modeList, modeRename:
		if m.searching {
			return m, nil
		}
		return m, m.moveCursor(delta)
	default:
		return m, nil
	}
}

// resizeGrip is the 1-column accent bar drawn between the panels while
// resize mode is active so the drag target is obvious.
func (m *Model) resizeGrip(height int) string {
	style := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	if m.splitDragging {
		style = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	}
	if height < 1 {
		height = 1
	}
	lines := make([]string, height)
	for i := range lines {
		lines[i] = style.Render("║")
	}
	return strings.Join(lines, "\n")
}
