package ui

import (
	"io"
	"os"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
)

// cursorAnchor is the real terminal cell an input method should open beside.
// Bubble Tea v1 finishes every alternate-screen render at the bottom-left,
// while focus mode paints a pane caret elsewhere, so an IME otherwise follows
// that renderer cursor instead of the caret the user is typing at. The cell
// itself lives in ui/frame, beside the writer that publishes it.
type cursorAnchor struct {
	cell uiframe.CursorAnchor
}

func (a *cursorAnchor) set(col, row int, ok bool) { a.cell.Set(col, row, ok) }

func (a *cursorAnchor) get() (col, row int, ok bool) { return a.cell.Get() }

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
		m.focus.imeCursor.set(col, row, ok)
		return uiframe.StripCursorMarker(frame)
	}
	frame, col, row, ok := uiframe.CursorMarkerPosition(frame)
	m.focus.imeCursor.set(col, row, ok)
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

func textInputView(input textinput.Model) string { return uiframe.TextInputView(input) }

func textAreaView(input textarea.Model) string { return uiframe.TextAreaView(input) }

// CursorOutput keeps native TTY detection and terminal sizing while placing
// the host cursor at the active input caret after each rendered frame.
func (m *Model) CursorOutput(output *os.File) io.Writer {
	if m.focus.imeCursor == nil {
		m.focus.imeCursor = &cursorAnchor{}
	}
	return uiframe.NewCursorTTYOutput(output, &m.focus.imeCursor.cell)
}
