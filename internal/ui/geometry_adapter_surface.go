package ui

import (
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	tea "github.com/charmbracelet/bubbletea"
)

// geometryRoot is the root as the resize gesture reaches it. The gesture
// state stays on the root: the mouse drags the same divider, and the seam
// and the status line read it while the surface is up.
type geometryRoot struct{ *Model }

func (r geometryRoot) Gesture() uigeometry.Gesture {
	split := r.layout.split
	return uigeometry.Gesture{
		Ratio:       split.ratio,
		RatioBefore: split.ratioBefore,
		ResizeMode:  split.resizeMode,
		Dragging:    split.dragging,
		Moved:       split.moved,
	}
}

func (r geometryRoot) SetGesture(gesture uigeometry.Gesture) {
	r.layout.split.ratio = gesture.Ratio
	r.layout.split.ratioBefore = gesture.RatioBefore
	r.layout.split.resizeMode = gesture.ResizeMode
	r.layout.split.dragging = gesture.Dragging
	r.layout.split.moved = gesture.Moved
}

func (r geometryRoot) Exit(commit bool) { r.exitResizeMode(commit) }

func (r geometryRoot) Nudge(delta int) { r.nudgeSplit(delta) }

func (r geometryRoot) Quit() tea.Cmd { return r.quit() }

func (r geometryRoot) TakeForeground() { r.takeForeground() }

func (r geometryRoot) ListMouse(msg tea.MouseMsg) tea.Cmd {
	_, cmd := r.listMouse(msg)
	return cmd
}

func newResizeSurface(m *Model) *uigeometry.Surface {
	return uigeometry.NewSurface(geometryRoot{m}, m.services.listKeys)
}

var _ uigeometry.Root = geometryRoot{}
