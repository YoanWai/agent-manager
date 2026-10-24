package ui

import (
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	tea "github.com/charmbracelet/bubbletea"
)

// resizeSessions syncs every live session's tmux window to the preview
// panel's pixel box so a capture fills the preview 1:1, resizing in
// parallel so a fleet of sessions does not serialize N tmux round-trips
// on the UI path. Width changes and height growth pin eagerly; a box that
// lost rows, to transient chrome or a shorter terminal, leaves the pane
// tall and lets paneWindow crop the view instead, because a height shrink
// makes Codex clear the pane's entire scrollback (#369).
func (m *Model) resizeSessions() {
	width, height := m.paneTargetSize()
	if width <= 0 || height <= 0 {
		return
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
	targets := m.focus.runtime.ResizeTargets(focusruntime.Plan{
		Box:         [2]int{width, height},
		Sessions:    m.workspace.sessions,
		Panes:       m.workspace.panes,
		FitsHeight:  func(tool string) bool { return m.services.cfg.Tools[tool].FitsHeight },
		FullFocusID: fullFocusID,
	})
	if len(targets) == 0 {
		return
	}
	request := geometryRequest{}
	for _, target := range targets {
		request.targets = append(request.targets, paneResize{id: target.ID, size: [2]int{width, target.Height}})
	}
	m.queueGeometry(request)
}

// pinFullFocusPane sizes a session opened full screen to the whole
// terminal body, the reflow an attach performs, so the capture fills the
// full width frame 1:1. Returning to the list leaves the pane this size:
// shrinking it back would cost a Codex agent its scrollback (#369), and
// paneWindow already crops a taller pane from its bottom.
func (m *Model) pinFullFocusPane(id string) {
	width, height := m.layout.width, m.listBodyHeight()
	if width <= 0 || height <= 0 {
		return
	}
	if last, sized := m.focus.runtime.SizeOf(id); sized && last[0] == width && last[1] >= height {
		return
	}
	m.queueGeometry(geometryRequest{targets: []paneResize{{id: id, size: [2]int{width, height}}}})
}

// markFreshPane queues one exact size pin for a session whose window this
// run just created. Its launch size came from pre-selection geometry, and
// with nothing in its scrollback yet the one re-pin is free, unlike the
// panes adopted from a previous run, which seedPaneGeom protects.
func (m *Model) markFreshPane(id string) { m.focus.runtime.PinOwed(id) }

// publishPaneSize records the box for the launch paths that run without a
// manager: the CLI and the MCP server open a pane with nothing to ask for
// the preview geometry, and tmux gives an unsized detached session 80x24.
func (m *Model) publishPaneSize() {
	if m.layout.width <= 0 {
		return
	}
	width, height := m.paneTargetSize()
	if width <= 0 || height <= 0 || m.focus.runtime.PublishedSize() == [2]int{width, height} {
		return
	}
	m.queueGeometry(geometryRequest{publish: [2]int{width, height}})
}

// markReplacedPanesFresh spots a session whose pane process changed since
// its geometry was measured: a revive outside this process put the row on
// a window the manager never sized.
func (m *Model) markReplacedPanesFresh() { m.focus.runtime.MarksFresh(m.workspace.panes) }

// seedPaneGeom fills the geometry cache from the pass's pane listing for
// sessions this run has not sized yet, so a pane adopted from a previous
// run is not shrunk (and its Codex scrollback cleared) just to match a box
// it already exceeds.
func (m *Model) seedPaneGeom() { m.focus.runtime.Seed(m.workspace.panes) }

func (m *Model) routeResizeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Resuming from a tmux attach re-sends the current size unchanged; only
		// a real resize needs the per-session tmux resize calls, so an
		// unchanged size skips them and keeps detach latency flat.
		if msg.Width == m.layout.width && msg.Height == m.layout.height {
			return routed(m, nil)
		}
		m.layout.width = msg.Width
		m.layout.height = msg.Height
		// Re-assert the terminal backdrop: a reattach or a fresh outer
		// terminal delivers a size message and may carry stale colors.
		SyncTerminalColors()
		m.publishPaneSize()
		m.resizeSessions()
		if m.fullFocus() {
			if sess, ok := m.selected(); ok {
				m.pinFullFocusPane(sess.ID)
			}
		}
		m.syncFormFieldWidths()
		return routed(m, nil)
	}
	return nil, nil, false
}
