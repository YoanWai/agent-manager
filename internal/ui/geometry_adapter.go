package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	tea "github.com/charmbracelet/bubbletea"
)

// splitState is the horizontal sessions/sidebar split as the mouse, the
// resize surface and the status line read it.
func (m *Model) geometryLayout() uigeometry.Layout { return geometryHost{m}.Layout() }

func (m *Model) splitWidths() (int, int) {
	return m.geometryLayout().SplitWidths(m.layout.split.Ratio)
}

func (m *Model) previewPaneWidth() int {
	return m.geometryLayout().PreviewWidth(m.layout.split.Ratio)
}

func (m *Model) paneTargetSize() (int, int) {
	return m.geometryLayout().PaneSize(m.layout.split.Ratio)
}

func (m *Model) listChromeRows() int { return m.geometryLayout().ChromeRows() }

func (m *Model) listBodyHeight() int { return m.geometryLayout().BodyHeight() }

func (m *Model) previewPaneHeight() int {
	return m.geometryLayout().PreviewHeight(m.layout.split.Ratio)
}

func (m *Model) bodyYRange() (int, int) { return m.geometryLayout().BodyRange() }

// fullFocus reports whether the focused session owns the whole terminal
// body, which is how the full screen layout opens a session.
func (m *Model) fullFocus() bool {
	return m.prefs.fullLayout && m.focused()
}

// focusPaneRows is the rows of pane content the focused view paints.
func (m *Model) focusPaneRows() int {
	return uigeometry.FocusPaneRows(m.geometryLayout(), m.layout.split.Ratio)
}

// dividerX is the column index of the sessions/sidebar junction.
func (m *Model) dividerX() int {
	return m.layout.split.Divider(m.layout.width)
}

// setSplitFromX pins the left panel's right edge to terminal column x.
func (m *Model) setSplitFromX(x int) {
	m.applyGesture(m.layout.split.Drag(x, m.layout.width))
}

// enterResizeMode arms divider dragging, which the arrow keys drive.
func (m *Model) enterResizeMode() (tea.Model, tea.Cmd) {
	if !m.listClear() || m.rail.Busy() || m.quick() != nil {
		return m, nil
	}
	gesture := m.layout.split
	gesture.ResizeMode = true
	gesture.Dragging = false
	gesture.RatioBefore = gesture.Ratio
	m.applyGesture(gesture)
	m.push(newResizeSurface(m))
	m.clearErr()
	return m, nil
}

// endDrag drops a press on the divider that moved nothing.
func (m *Model) endDrag() {
	m.applyGesture(m.layout.split.Released(0, 0))
	if !m.layout.split.ResizeMode {
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

// exitResizeMode leaves divider-drag mode: commit persists the ratio, cancel
// puts back the one it started from.
func (m *Model) exitResizeMode(commit bool) (tea.Model, tea.Cmd) {
	if !m.layout.split.ResizeMode && !m.layout.split.Dragging {
		return m, nil
	}
	m.applyGesture(m.layout.split.Ended(commit))
	if commit {
		m.persistSplitRatio()
	}
	m.dropResize()
	m.resizeSessions()
	return m, nil
}

func (m *Model) nudgeSplit(delta int) {
	m.applyGesture(m.layout.split.Nudged(delta, m.layout.width))
}

// prepareSplitForQuit accepts the live preview as the user's final ratio. It
// runs only after requestQuit has passed the installer refusal gates.
func (m *Model) prepareSplitForQuit() {
	if !m.layout.split.ResizeMode && !m.layout.split.Dragging {
		return
	}
	m.persistSplitRatio()
	m.applyGesture(uigeometry.Gesture{Ratio: m.layout.split.Ratio})
	m.dropResize()
}

func (m *Model) applyGesture(gesture uigeometry.Gesture) { m.layout.split = gesture }

func (m *Model) routeResizeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	size, ok := msg.(tea.WindowSizeMsg)
	if !ok {
		return nil, nil, false
	}
	// Resuming from a tmux attach re-sends the current size unchanged; only a
	// real resize needs the per-session tmux resize calls.
	if size.Width == m.layout.width && size.Height == m.layout.height {
		return routed(m, nil)
	}
	m.layout.width = size.Width
	m.layout.height = size.Height
	// Re-assert the terminal backdrop: a reattach or a fresh outer terminal
	// delivers a size message and may carry stale colors.
	SyncTerminalColors()
	m.publishPaneSize()
	m.resizeSessions()
	if m.fullFocus() {
		if sess, ok := m.selected(); ok {
			m.layout.panes.PinFullFocus(paneSizing{m}, sess.ID, m.layout.width, m.listBodyHeight())
		}
	}
	m.syncFormFieldWidths()
	return routed(m, nil)
}

func (m *Model) applyGeometryEffect(result uigeometrypanes.Result, err error) {
	m.layout.panes.Apply(result)
	if err != nil {
		m.reportErr(err.Error())
	}
}

// persistSplitRatio captures the current ratio for the ordered effect lane so
// the next launch reopens at the same split without holding Update on SQLite.
func (m *Model) persistSplitRatio() {
	if m.services.store == nil {
		return
	}
	m.layout.panes.Persist(paneSizing{m}, m.layout.split.Ratio)
}

// queueGeometry queues a resize, dropping what the lane already carries and
// folding into the geometry job queued just before it, never across an
// accepted mutation.
func (m *Model) queueGeometry(request uigeometrypanes.Resize) {
	request = uigeometrypanes.Effective(request, m.queuedGeometry())
	if request.Empty() {
		return
	}
	if n := len(m.effects.Main.Pending); n > 0 {
		if previous, ok := m.effects.Main.Pending[n-1].Request.(uigeometrypanes.ResizeRequest); ok {
			request = uigeometrypanes.Merge(previous.Resize, request)
			m.effects.Main.Pending = m.effects.Main.Pending[:n-1]
		}
	}
	m.enqueueEffect(uigeometrypanes.ResizeRequest{Resize: request}, 0, false)
}

// queuedGeometry reads the resizes already waiting on the lane, in the order
// the lane will run them.
func (m *Model) queuedGeometry() []uigeometrypanes.Resize {
	jobs := append([]*uieffects.Job(nil), m.effects.Main.Pending...)
	if m.effects.Main.Active != nil {
		jobs = append([]*uieffects.Job{m.effects.Main.Active}, jobs...)
	}
	queued := make([]uigeometrypanes.Resize, 0, len(jobs))
	for _, job := range jobs {
		if request, ok := job.Request.(uigeometrypanes.ResizeRequest); ok {
			queued = append(queued, request.Resize)
		}
	}
	return queued
}

// resizeSessions syncs every live session's tmux window to the preview panel's
// pixel box so a capture fills the preview 1:1.
func (m *Model) resizeSessions() {
	width, height := m.paneTargetSize()
	// The session open full screen is pinned to the whole body by
	// PinFullFocus, not to the preview box; matching it to the box here would
	// shrink its height and clear its scrollback mid-focus.
	fullFocusID := ""
	if m.fullFocus() {
		if sess, ok := m.selected(); ok {
			fullFocusID = sess.ID
		}
	}
	sessions := make([]uigeometrypanes.Session, 0, len(m.workspace.sessions))
	for _, sess := range m.workspace.sessions {
		sessions = append(sessions, uigeometrypanes.Session{
			ID:        sess.ID,
			Tool:      sess.Tool,
			AltScreen: m.workspace.panes[sess.ID].AltScreen,
			Archived:  sess.Archived,
		})
	}
	m.layout.panes.QueueAll(paneSizing{m}, uigeometrypanes.Box{
		Width:       width,
		Height:      height,
		FullFocusID: fullFocusID,
	}, sessions, m.workspace.panes)
}

// markFreshPane queues one exact size pin for a session whose window this run
// just created.
func (m *Model) markFreshPane(id string) { m.layout.panes.MarkFresh(id) }

// publishPaneSize records the box for the launch paths that run without a
// manager.
func (m *Model) publishPaneSize() {
	if m.layout.width <= 0 {
		return
	}
	width, height := m.paneTargetSize()
	m.layout.panes.Publish(paneSizing{m}, width, height)
}

// adoptPaneGeom takes a session off the geometry cache, so its next pin is
// exact: an attach resized its window to the whole terminal and tmux keeps
// that size on detach.
func (m *Model) adoptPaneGeom(id string) {
	m.layout.panes.Forget(id)
	width, height := m.paneTargetSize()
	m.queueGeometry(uigeometrypanes.Resize{Targets: []uigeometrypanes.PaneResize{{ID: id, Size: [2]int{width, height}}}})
}
