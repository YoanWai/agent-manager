package ui

import (
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	tea "github.com/charmbracelet/bubbletea"
)

// splitState is the horizontal sessions/sidebar split as the mouse, the
// resize surface and the status line read it. The gesture stays on the root
// because mouse.go drives the same divider the surface does.
type splitState struct {
	ratio       float64
	ratioBefore float64
	resizeMode  bool
	dragging    bool
	moved       bool
}

// splitHitSlop is how many columns past the junction count as the divider,
// on top of the junction itself.
const splitHitSlop = uigeometry.SplitHitSlop

// splitWidths is the body's horizontal split: the sessions panel takes ratio
// of the width, floored so both sides stay usable.
func (m *Model) splitWidths() (int, int) {
	return uigeometry.Widths(m.layout.split.ratio, m.layout.width)
}

// persistSplitRatio captures the current ratio for the ordered effect lane so
// the next launch reopens at the same split without holding Update on SQLite.
func (m *Model) persistSplitRatio() {
	if m.services.store == nil {
		return
	}
	m.layout.panes.Persist(paneSizing{m}, m.layout.split.ratio)
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

// paneSizing is the root as the geometry runtime reaches it: what a shrink
// costs a tool, the effect lane a pin rides, and the ratio save it starts.
type paneSizing struct{ *Model }

func (r paneSizing) FitsHeight(tool string) bool { return r.services.cfg.Tools[tool].FitsHeight }

func (r paneSizing) Queue(request uigeometrypanes.Resize) { r.queueGeometry(request) }

func (r paneSizing) SaveSplitRatio(value string) uint64 {
	previousID := r.effects.NextID
	r.enqueueEffect(splitSaveRequest{value: value}, 0, false)
	if r.effects.NextID == previousID {
		return 0
	}
	return r.effects.NextID
}

func (r paneSizing) ReportErr(text string) { r.reportErr(text) }

func (r paneSizing) ClearErr() { r.clearErr() }

func (r paneSizing) ErrorIs(text string) bool { return r.errBar.text == text }

var _ uigeometrypanes.Root = paneSizing{}
