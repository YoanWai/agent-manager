package ui

import (
	"strings"

	uistatusbar "github.com/YoanWai/agent-manager/internal/ui/statusbar"
)

func statusbarTones() uistatusbar.Tones { return uistatusbar.TonesFor(current()) }

// statusLine is the transient message: prompts, search, and self-dismissing
// errors. It floats in a card over the frame rather than taking a row, so the
// body keeps its height whether or not a notice is up.
func (m *Model) statusLine() string {
	focusStatus := m.focus.pane.Status()
	pagesScrollback := false
	if sess, ok := m.selected(); ok && m.focus.pane.PagesScrollback(sess.ID) {
		pagesScrollback = true
	}
	return uistatusbar.Line(uistatusbar.Message{
		Report:          m.errBar.text,
		Kind:            m.errBar.kind(),
		Focused:         m.focused(),
		ScrolledBack:    m.focus.pane.ScrolledBack(),
		ScrollOffset:    focusStatus.ScrollOffset,
		PagesScrollback: pagesScrollback,
		Copied:          focusStatus.CopiedChars,
		ResizeMode:      m.layout.split.resizeMode,
		Dragging:        m.layout.split.dragging,
		Notice:          m.review.Snapshot().Notice,
	}, m.glyphs(), statusbarTones())
}

// statusMessage styles whatever sits on the status bar: an action that went
// through reads as an outcome, one that went through with a caveat as a
// warning, everything else as a failure, in the glyphs the calling surface
// marks the three with.
func (m *Model) statusMessage(fail, done, warn string) string {
	return m.errBar.kind().Paint(m.errBar.text, uistatusbar.Marks{Fail: fail, Done: done, Warn: warn}, statusbarTones())
}

// statusToast is the transient message rendered as a card that floats over
// the frame's top right, so a notice never costs the body a row.
func (m *Model) statusToast() []string {
	leftWidth, _ := m.splitWidths()
	return uistatusbar.Toast(m.statusLine(), m.layout.width-leftWidth-2, statusbarTones())
}

// overlayTopRight splices a pre-painted box over the frame's rows, anchored
// under the header band and inset from the right edge. Rows past the frame's
// bottom are dropped rather than extending it, so the layout never moves.
func (m *Model) overlayTopRight(frame string, box []string, top int) string {
	if len(box) == 0 {
		return frame
	}
	left := m.layout.width - maxLineWidth(box) - uistatusbar.Margin
	if left < 0 {
		left = 0
	}
	lines := strings.Split(frame, "\n")
	for i, patch := range box {
		row := top + i
		if row < 0 || row >= len(lines) {
			continue
		}
		lines[row] = uistatusbar.Splice(lines[row], patch, left)
	}
	return strings.Join(lines, "\n")
}

// spliceAtColumn overpaints a run of cells inside a styled row, keeping the
// escape state on both sides of the patch intact.
func spliceAtColumn(row, patch string, left int) string { return uistatusbar.Splice(row, patch, left) }
