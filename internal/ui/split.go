package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strconv"
)

// splitState is the horizontal sessions/sidebar split. ratio is the left
// panel's share of the terminal width; resizeMode arms keyboard divider
// nudging, dragging holds a divider drag whichever armed it, and moved
// separates a drag from a plain click on the seam, which commits nothing.
type splitState struct {
	ratio       float64
	ratioBefore float64
	resizeMode  bool
	dragging    bool
	moved       bool
}

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
	if m.services.store == nil {
		return
	}
	if err := m.services.store.SetSetting(splitRatioSetting, strconv.FormatFloat(m.split.ratio, 'f', 4, 64)); err != nil {
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
	if m.mode != modeList || m.rail.searching || m.quick.active {
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
