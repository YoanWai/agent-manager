package ui

import (
	"strconv"

	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	tea "github.com/charmbracelet/bubbletea"
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
	// saveID is the newest accepted persistence job. saveError remembers only
	// this adapter's visible failure so a later successful save can clear it
	// without erasing another feature's status.
	saveID    uint64
	saveError string
}

const (
	splitRatioSetting = uigeometry.SplitRatioSetting
	defaultSplitRatio = uigeometry.DefaultSplitRatio
	minSplitSide      = uigeometry.MinSplitSide
	splitHitSlop      = uigeometry.SplitHitSlop
)

// settingReader is the store surface loadSplitRatio needs; tests stub it.
type settingReader interface {
	Setting(key string) (string, error)
}

// loadSplitRatio restores the sessions/sidebar ratio, or the 30% default
// when nothing is stored or the value is unusable.
func loadSplitRatio(st settingReader) float64 {
	raw, err := st.Setting(splitRatioSetting)
	if err != nil {
		return defaultSplitRatio
	}
	return uigeometry.ParseRatio(raw)
}

func clampSplitLeft(left, width int) int { return uigeometry.ClampSplitLeft(left, width) }

func (m *Model) splitWidths() (int, int) {
	return uigeometry.Widths(m.layout.split.ratio, m.layout.width)
}

// persistSplitRatio captures the current ratio for the ordered effect lane so
// the next launch reopens at the same split without holding Update on SQLite.
func (m *Model) persistSplitRatio() {
	if m.services.store == nil {
		return
	}
	previousID := m.effects.nextID
	m.enqueueEffect(splitSaveRequest{value: strconv.FormatFloat(m.layout.split.ratio, 'f', 4, 64)}, 0, false)
	if m.effects.nextID != previousID {
		m.layout.split.saveID = m.effects.nextID
	}
}

// prepareSplitForQuit accepts the live preview as the user's final ratio.
// It runs only after requestQuit has passed the installer refusal gates, so a
// refused quit leaves the active resize interaction untouched.
func (m *Model) prepareSplitForQuit() {
	if !m.layout.split.resizeMode && !m.layout.split.dragging {
		return
	}
	m.persistSplitRatio()
	m.layout.split.resizeMode = false
	m.layout.split.dragging = false
	m.layout.split.moved = false
	m.dropResize()
}

// setSplitFromX pins the left panel's right edge to terminal column x and
// updates the stored ratio. Live during a drag; consumers re-read via
// splitWidths on the next View.
func (m *Model) setSplitFromX(x int) {
	if m.layout.width <= 0 {
		return
	}
	m.layout.split.ratio = uigeometry.RatioAt(x, m.layout.width)
}

// enterResizeMode arms divider dragging, which the arrow keys drive. The
// app holds mouse reporting, so a drag here reaches handleMouse too.
func (m *Model) enterResizeMode() (tea.Model, tea.Cmd) {
	if !m.listClear() || m.rail.Busy() || m.quick() != nil {
		return m, nil
	}
	m.layout.split.resizeMode = true
	m.layout.split.dragging = false
	m.layout.split.ratioBefore = m.layout.split.ratio
	m.push(newResizeSurface(m))
	m.clearErr()
	return m, nil
}

// endDrag drops a press on the divider that moved nothing. A keyboard
// resize stays armed; a drag alone takes its surface down.
func (m *Model) endDrag() {
	m.layout.split.dragging = false
	if !m.layout.split.resizeMode {
		m.dropResize()
	}
}

// dropResize takes the gesture's surface off the top of the stack.
func (m *Model) dropResize() {
	if top, ok := m.top(); ok {
		if _, resize := top.(*uigeometry.Surface); resize {
			m.Pop()
		}
	}
}

// exitResizeMode leaves divider-drag mode. When commit is true the current
// ratio is persisted; cancel restores the pre-mode ratio. Either path ends
// with a pane resize so the preview stays 1:1 with the panel.
func (m *Model) exitResizeMode(commit bool) (tea.Model, tea.Cmd) {
	if !m.layout.split.resizeMode && !m.layout.split.dragging {
		return m, nil
	}
	if !commit {
		m.layout.split.ratio = m.layout.split.ratioBefore
	} else {
		m.persistSplitRatio()
	}
	m.layout.split.dragging = false
	m.layout.split.moved = false
	m.layout.split.resizeMode = false
	m.dropResize()
	m.resizeSessions()
	return m, nil
}

// nudgeSplit moves the divider by delta columns while resize mode is on.
// UI reflows instantly; tmux reflow is deferred to commit (| / mouse up)
// so holding an arrow does not spawn a resize-window per keystroke.
func (m *Model) nudgeSplit(delta int) {
	if m.layout.width <= 0 || delta == 0 {
		return
	}
	left, _ := m.splitWidths()
	m.setSplitFromX(left + delta)
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
	return uigeometry.OnDivider(x, m.dividerX())
}
