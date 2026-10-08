package ui

import (
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
)

type focusRuntimeState struct {
	watch             *focusWatch
	imeCursor         *cursorAnchor
	lastPaneSizes     map[string][2]int
	lastPanePIDs      map[string]int
	lastPublishedSize [2]int
}

func (m *Model) routeFocusMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case linkOpenErrMsg:
		m.errBar.text = msg.err.Error()
		return routed(m, nil)

	case linkPageMsg:
		return routed(m, showLinkPage(msg.url))

	case focusCopiedMsg:
		// The clipboard writer runs off the update loop and can take
		// hundreds of milliseconds, long enough for a click elsewhere to
		// drop the highlight this count belongs to.
		if !m.focus.pane.ApplyCopied(msg.gen, msg.chars) {
			return routed(m, nil)
		}
		m.errBar.text = ""
		return routed(m, nil)

	case focusScrollMsg:
		currentID := ""
		if sess, ok := m.selected(); ok {
			currentID = sess.ID
		}
		result := m.focus.pane.ApplyRegion(uifocus.RegionResult{
			SessionID: msg.sessID,
			Offset:    msg.offset,
			Rows:      msg.rows,
			Preview:   msg.preview,
			OK:        msg.ok,
		}, currentID, m.focusPaneRows())
		if result.Next != nil {
			return routed(m, m.focusRegionRequestCmd(*result.Next))
		}
		if result.Apply {
			m.workspace.preview = result.Preview
		}
		return routed(m, nil)

	case focusPreviewMsg:
		currentID := ""
		if sess, ok := m.selected(); ok {
			currentID = sess.ID
		}
		result := m.focus.pane.ApplyPane(uifocus.PaneUpdate{
			SessionID: msg.sessID,
			Mouse:     msg.paneMouse,
			Alt:       msg.paneAlt,
			Motion:    msg.paneMotion,
			SGR:       msg.paneSGR,
			History:   msg.historySize,
			Cursor: uifocus.Cursor{
				X: msg.cursorX, Y: msg.cursorY,
				Visible: msg.cursorOK, PositionKnown: msg.paneStateOK,
			},
		}, currentID)
		if result.UsePreview {
			m.workspace.preview = msg.preview
		}
		return routed(m, nil)
	}
	return nil, nil, false
}
