package ui

import (
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uistatusbar "github.com/YoanWai/agent-manager/internal/ui/statusbar"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// errBar is what the manager last put on the status bar: the text, the run
// that named it an outcome or a warning, and the poll baseline that ages it
// out. Only reportDone and reportWarn set those runs; any later text alone
// leaves them behind, so a message says it worked or reads as a failure.
type errBar struct {
	text, done, warn, shown string
	age                     int
}

func (e errBar) kind() uistatusbar.Kind {
	switch {
	case e.text != "" && e.text == e.done:
		return uistatusbar.Success
	case e.text != "" && e.text == e.warn:
		return uistatusbar.Warning
	}
	return uistatusbar.Failure
}

func (e errBar) worked() bool { return e.kind() == uistatusbar.Success }
func (e errBar) warned() bool { return e.kind() == uistatusbar.Warning }

// reportErr puts a failure on the status bar; the poll ages it out.
func (m *Model) reportErr(text string) { m.errBar = errBar{text: text} }

// clearErr takes whatever message is on the status bar down.
func (m *Model) clearErr() { m.errBar = errBar{} }

// reportDone puts an action that went through on the status bar.
func (m *Model) reportDone(text string) { m.errBar = errBar{text: text, done: text} }

// reportWarn puts an action that went through with a caveat on the status
// bar.
func (m *Model) reportWarn(text string) { m.errBar = errBar{text: text, warn: text} }

type errMsg struct{ err error }

// ageError counts one poll against the message. Text that just changed
// starts over, and text that survived two ticks clears, so a transient
// message self-dismisses without a timer per callsite.
func (m *Model) ageError() {
	e := &m.errBar
	switch {
	case e.text == "":
		*e = errBar{}
	case e.text != e.shown:
		e.shown, e.age = e.text, 0
	default:
		if e.age++; e.age >= 2 {
			*e = errBar{}
		}
	}
}

func (m *Model) routeStatusMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if err, ok := msg.(errMsg); ok {
		m.reportErr(err.err.Error())
		return routed(m, nil)
	}
	return nil, nil, false
}

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
		GlyphMode:       m.prefs.glyphMode,
		Report:          m.errBar.text,
		Kind:            m.errBar.kind(),
		Focused:         m.focused(),
		ScrolledBack:    m.focus.pane.ScrolledBack(),
		ScrollOffset:    focusStatus.ScrollOffset,
		PagesScrollback: pagesScrollback,
		Copied:          focusStatus.CopiedChars,
		Link:            m.hoveredLink(),
		ResizeMode:      m.layout.split.ResizeMode,
		Dragging:        m.layout.split.Dragging,
		Notice:          m.review.Snapshot().Notice,
	}, m.glyphs(), uistatusbar.TonesFor(uitheme.Current()))
}

// statusMessage styles whatever sits on the status bar: an action that went
// through reads as an outcome, one that went through with a caveat as a
// warning, everything else as a failure, in the glyphs the calling surface
// marks the three with.
func (m *Model) statusMessage(fail, done, warn string) string {
	return m.errBar.kind().Paint(m.errBar.text, uistatusbar.Marks{Fail: fail, Done: done, Warn: warn}, uistatusbar.TonesFor(uitheme.Current()))
}

// statusToast is the transient message rendered as a card that floats over
// the frame's top right, so a notice never costs the body a row.
func (m *Model) statusToast() []string {
	leftWidth, _ := m.splitWidths()
	return uistatusbar.Toast(m.statusLine(), m.layout.width-leftWidth-2, uistatusbar.TonesFor(uitheme.Current()), m.prefs.glyphMode)
}

// overlayTopRight splices a pre-painted box over the frame's rows, anchored
// under the header band and inset from the right edge. Rows past the frame's
// bottom are dropped rather than extending it, so the layout never moves.
func (m *Model) overlayTopRight(frame string, box []string, top int) string {
	if len(box) == 0 {
		return frame
	}
	left := m.layout.width - uiframe.MaxLineWidth(box) - uistatusbar.Margin
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
