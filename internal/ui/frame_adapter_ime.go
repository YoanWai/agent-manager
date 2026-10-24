package ui

import (
	"io"
	"os"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
)

// syncCursorAnchor publishes the input coordinates in a finished frame and
// removes the private marker before Bubble Tea writes the frame. Focus mode
// uses its recorded pane box as a fallback because its caret is painted over
// captured terminal output rather than by a Bubbles input widget.
func (m *Model) syncCursorAnchor(frame string) string {
	if m.focus.imeCursor == nil {
		return uiframe.StripCursorMarker(frame)
	}
	if m.agentOwnsKeyboard() {
		col, row, ok := m.focusCursorAnchor()
		m.focus.imeCursor.Set(col, row, ok)
		return uiframe.StripCursorMarker(frame)
	}
	frame, col, row, ok := uiframe.CursorMarkerPosition(frame)
	m.focus.imeCursor.Set(col, row, ok)
	return frame
}

func (m *Model) focusCursorAnchor() (col, row int, ok bool) {
	if !m.focused() {
		return 0, 0, false
	}
	sess, selected := m.selected()
	if !selected {
		return 0, 0, false
	}
	return m.focus.pane.CursorAnchor(sess.ID)
}

// CursorOutput keeps native TTY detection and terminal sizing while placing
// the host cursor at the active input caret after each rendered frame.
func (m *Model) CursorOutput(output *os.File) io.Writer {
	if m.focus.imeCursor == nil {
		m.focus.imeCursor = &uiframe.CursorAnchor{}
	}
	return uiframe.NewCursorTTYOutput(output, m.focus.imeCursor)
}
