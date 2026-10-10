package ui

import (
	"fmt"
)

// statusLine is the transient message: prompts, search, and self-dismissing
// errors. It floats in a card over the frame rather than taking a row, so the
// body keeps its height whether or not a notice is up.
func (m *Model) statusLine() string {
	focusStatus := m.focus.pane.Status()
	switch {
	// Errors outrank the focus notices: a scrolled or focused pane must
	// not hide a failure report.
	case m.mode == modeFocus && m.errBar.text != "":
		return m.statusMessage("✕", "●", "▲")
	case m.focus.pane.ScrolledBack():
		catchUp := "wheel down or type"
		if sess, ok := m.selected(); ok && m.focus.pane.PagesScrollback(sess.ID) {
			catchUp = "wheel down, pgdn or type"
		}
		return keyStyle.Render("scrolled ") +
			subtleStyle.Render(fmt.Sprintf("%d lines back · %s to catch up", focusStatus.ScrollOffset, catchUp))
	case m.mode == modeFocus && focusStatus.CopiedChars > 0:
		return keyStyle.Render("copied ") +
			subtleStyle.Render(fmt.Sprintf("%d chars to clipboard", focusStatus.CopiedChars))
	case m.layout.split.resizeMode || m.layout.split.dragging:
		hint := "←→ resize · drag divider · enter set · esc cancel"
		if m.layout.split.dragging {
			hint = "release to set · esc cancels"
		}
		return keyStyle.Render("resize ") + subtleStyle.Render(hint)
	case m.errBar.text != "":
		return m.statusMessage("✕", "●", "▲")
	case m.review.Snapshot().Notice != "":
		return doneStyle.Render("● " + escapeControlsInline(m.review.Snapshot().Notice))
	default:
		return ""
	}
}

// statusMessage styles whatever sits on the status bar: an action that went
// through reads as an outcome, one that went through with a caveat as a
// warning, everything else as a failure, in the glyphs the calling surface
// marks the three with.
func (m *Model) statusMessage(fail, done, warn string) string {
	text := escapeControlsInline(m.errBar.text)
	switch {
	case m.errBar.worked():
		return doneStyle.Render(done + " " + text)
	case m.errBar.warned():
		return warnStyle.Render(warn + " " + text)
	}
	return errStyle.Render(fail + " " + text)
}
