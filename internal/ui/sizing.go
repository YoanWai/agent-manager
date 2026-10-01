package ui

// resizeSessions syncs every live session's tmux window to the preview
// panel's pixel box so a capture fills the preview 1:1, resizing in
// parallel so a fleet of sessions does not serialize N tmux round-trips
// on the UI path. Width changes and height growth pin eagerly; a box that
// lost rows, to transient chrome or a shorter terminal, leaves the pane
// tall and lets paneWindow crop the view instead, because a height shrink
// makes Codex clear the pane's entire scrollback (#369). A pane on the
// alternate screen holds no scrollback for a shrink to clear, and a tool
// with fits_height keeps its own, so those follow the box down and a
// full-screen TUI keeps its top rows in view.
func (m *Model) resizeSessions() {
	width, height := m.paneTargetSize()
	if width <= 0 || height <= 0 {
		return
	}
	if m.focusRuntime.lastPaneSizes == nil {
		m.focusRuntime.lastPaneSizes = map[string][2]int{}
	}
	m.markReplacedPanesFresh()
	m.seedPaneGeom()
	// The session open full screen is pinned to the whole body by
	// pinFullFocusPane, not to the preview box; matching it to the box
	// here would shrink its height and clear its scrollback mid-focus.
	fullFocusID := ""
	if m.fullFocus() {
		if sess, ok := m.selected(); ok {
			fullFocusID = sess.ID
		}
	}
	type target struct {
		id     string
		height int
	}
	var todo []target
	for _, sess := range m.workspace.sessions {
		if sess.Archived || sess.ID == fullFocusID {
			continue
		}
		wanted := height
		if last, ok := m.focusRuntime.lastPaneSizes[sess.ID]; ok {
			fitsHeight := m.workspace.panes[sess.ID].AltScreen || m.services.cfg.Tools[sess.Tool].FitsHeight
			keepsHeight := last[1] > height && !fitsHeight
			if last[0] == width && (last[1] == height || keepsHeight) {
				continue
			}
			// A width re-pin of a taller pane keeps its height: shrinking
			// it would clear a Codex scrollback (#369); the painted view
			// crops instead.
			if keepsHeight {
				wanted = last[1]
			}
		}
		todo = append(todo, target{id: sess.ID, height: wanted})
	}
	if len(todo) == 0 {
		return
	}
	request := geometryRequest{}
	for _, target := range todo {
		request.targets = append(request.targets, paneResize{id: target.id, size: [2]int{width, target.height}})
	}
	m.queueGeometry(request)
}

// pinFullFocusPane sizes a session opened full screen to the whole
// terminal body, the reflow an attach performs, so the capture fills the
// full width frame 1:1. Returning to the list leaves the pane this size:
// shrinking it back would cost a Codex agent its scrollback (#369), and
// paneWindow already crops a taller pane from its bottom.
func (m *Model) pinFullFocusPane(id string) {
	width, height := m.width, m.listBodyHeight()
	if width <= 0 || height <= 0 {
		return
	}
	if m.focusRuntime.lastPaneSizes == nil {
		m.focusRuntime.lastPaneSizes = map[string][2]int{}
	}
	if last, ok := m.focusRuntime.lastPaneSizes[id]; ok && last[0] == width && last[1] >= height {
		return
	}
	m.queueGeometry(geometryRequest{targets: []paneResize{{id: id, size: [2]int{width, height}}}})
}

// markFreshPane queues one exact size pin for a session whose window this
// run just created. Its launch size came from pre-selection geometry, and
// with nothing in its scrollback yet the one re-pin is free, unlike the
// panes adopted from a previous run, which seedPaneGeom protects.
func (m *Model) markFreshPane(id string) {
	if m.focusRuntime.lastPaneSizes == nil {
		m.focusRuntime.lastPaneSizes = map[string][2]int{}
	}
	m.focusRuntime.lastPaneSizes[id] = [2]int{0, 0}
}

// publishPaneSize records the box for the launch paths that run without a
// manager: the CLI and the MCP server open a pane with nothing to ask for
// the preview geometry, and tmux gives an unsized detached session 80x24.
func (m *Model) publishPaneSize() {
	if m.width <= 0 {
		return
	}
	width, height := m.paneTargetSize()
	if width <= 0 || height <= 0 || m.focusRuntime.lastPublishedSize == [2]int{width, height} {
		return
	}
	m.queueGeometry(geometryRequest{publish: [2]int{width, height}})
}

// markReplacedPanesFresh spots a session whose pane process changed since
// its geometry was measured: a revive outside this process put the row on
// a window the manager never sized, and the cached size of the window it
// replaced would otherwise read as already matching the box, leaving the
// new pane at whatever it was born with for the life of the run. The
// replacement has no scrollback to protect, so it takes markFreshPane's
// exact pin rather than the adopted-pane treatment that keeps a taller
// height.
func (m *Model) markReplacedPanesFresh() {
	if m.focusRuntime.lastPanePIDs == nil {
		m.focusRuntime.lastPanePIDs = map[string]int{}
	}
	for id, pane := range m.workspace.panes {
		last := m.focusRuntime.lastPanePIDs[id]
		m.focusRuntime.lastPanePIDs[id] = pane.PID
		if last != 0 && last != pane.PID {
			m.markFreshPane(id)
		}
	}
}

// seedPaneGeom fills the geometry cache from the pass's pane listing for
// sessions this run has not sized yet, so a pane adopted from a previous
// run is not shrunk (and its Codex scrollback cleared) just to match a box
// it already exceeds. A split window is taken every pass instead: the agent
// opened those panes out of its own window, taking room from the pane the
// preview draws, and no manager action precedes that for the cache to
// follow. Drift the manager did not cause otherwise stays, so a window
// someone resized from another client is left where they put it.
func (m *Model) seedPaneGeom() {
	for id, geom := range m.workspace.panes {
		last, sized := m.focusRuntime.lastPaneSizes[id]
		// markFreshPane's pin is still owed: a session created this run
		// carries its pre-selection launch size, not the box.
		if sized && last == [2]int{0, 0} {
			continue
		}
		if sized && geom.Panes < 2 {
			continue
		}
		m.focusRuntime.lastPaneSizes[id] = [2]int{geom.Width, geom.Height}
	}
}
