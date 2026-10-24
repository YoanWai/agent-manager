package ui

import (
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	tea "github.com/charmbracelet/bubbletea"
)

// resizeSessions syncs every live session's tmux window to the preview
// panel's pixel box so a capture fills the preview 1:1.
func (m *Model) resizeSessions() {
	width, height := m.paneTargetSize()
	// The session open full screen is pinned to the whole body by
	// PinFullFocus, not to the preview box; matching it to the box
	// here would shrink its height and clear its scrollback mid-focus.
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
				m.layout.panes.PinFullFocus(paneSizing{m}, sess.ID, m.layout.width, m.listBodyHeight())
			}
		}
		m.syncFormFieldWidths()
		return routed(m, nil)
	}
	return nil, nil, false
}
