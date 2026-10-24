package ui

import (
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// geometryHost is what the geometry package asks the root: the terminal box,
// what its chrome costs, and the divider gesture the mouse and the surface
// both move.
type geometryHost struct{ m *Model }

func (h geometryHost) Layout() uigeometry.Layout {
	return uigeometry.Layout{
		Width:           h.m.layout.width,
		Height:          h.m.layout.height,
		FullLayout:      h.m.prefs.fullLayout,
		FullFocus:       h.m.fullFocus(),
		HeaderRows:      h.m.headerRows(),
		FooterRows:      lipgloss.Height(h.m.viewFooter()),
		QuickFooterRows: h.quickFooterRows(),
		QuickBar:        h.m.quick() != nil,
		Detail:          func(inner int) int { return lipgloss.Height(h.m.viewDetail(inner)) },
	}
}

// quickFooterRows is the taller footer the quick bar paints, or zero when no
// bar is up and the resting footer is the painted one.
func (h geometryHost) quickFooterRows() int {
	if h.m.quick() == nil {
		return 0
	}
	return lipgloss.Height(h.m.listFooter())
}

func (h geometryHost) Gesture() uigeometry.Gesture {
	split := h.m.layout.split
	return uigeometry.Gesture{
		Ratio:       split.ratio,
		RatioBefore: split.ratioBefore,
		ResizeMode:  split.resizeMode,
		Dragging:    split.dragging,
		Moved:       split.moved,
	}
}

func (h geometryHost) SetGesture(gesture uigeometry.Gesture) {
	split := &h.m.layout.split
	split.ratio = gesture.Ratio
	split.ratioBefore = gesture.RatioBefore
	split.resizeMode = gesture.ResizeMode
	split.dragging = gesture.Dragging
	split.moved = gesture.Moved
}

func (h geometryHost) Exit(commit bool) { h.m.exitResizeMode(commit) }

func (h geometryHost) Nudge(delta int) { h.m.nudgeSplit(delta) }

func (h geometryHost) ListMouse(msg tea.MouseMsg) tea.Cmd {
	_, cmd := h.m.listMouse(msg)
	return cmd
}

func (h geometryHost) TakeForeground() { h.m.takeForeground() }

func (h geometryHost) Quit() tea.Cmd { return h.m.quit() }

func newResizeSurface(m *Model) *uigeometry.Surface {
	return uigeometry.NewSurface(geometryHost{m}, m.services.listKeys)
}

// paneSizing is the root as the geometry runtime reaches it: what a shrink
// costs a tool, the effect lane a pin rides, and the ratio save it starts.
type paneSizing struct{ *Model }

func (r paneSizing) FitsHeight(tool string) bool { return r.services.cfg.Tools[tool].FitsHeight }

func (r paneSizing) Queue(request uigeometrypanes.Resize) { r.queueGeometry(request) }

func (r paneSizing) SaveSplitRatio(value string) uint64 {
	previousID := r.effects.NextID
	r.enqueueEffect(uigeometrypanes.SplitSaveRequest{Value: value}, 0, false)
	if r.effects.NextID == previousID {
		return 0
	}
	return r.effects.NextID
}

func (r paneSizing) ReportErr(text string) { r.reportErr(text) }

func (r paneSizing) ClearErr() { r.clearErr() }

func (r paneSizing) ErrorIs(text string) bool { return r.errBar.text == text }

var (
	_ uigeometry.Root      = geometryHost{}
	_ uigeometrypanes.Root = paneSizing{}
)
