package ui

import (
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uistatusbar "github.com/YoanWai/agent-manager/internal/ui/statusbar"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

type errBar struct {
	text  string
	done  string
	warn  string
	shown string
	age   int
}

// outcome is the bar as the statusbar package reads it: what it says, the run
// that named it, and the poll count that ages it out.
func (e errBar) outcome() uistatusbar.Outcome {
	return uistatusbar.Told(e.text, e.done, e.warn, e.shown, e.age)
}

// kind is how the message reads, which is the tone the bar paints it in.
func (e errBar) kind() uistatusbar.Kind { return e.outcome().KindOf() }

// worked and warned are that kind as the yes/no question a caller asks when it
// only needs one branch.
func (e errBar) worked() bool { return e.kind() == uistatusbar.Success }
func (e errBar) warned() bool { return e.kind() == uistatusbar.Warning }

// fromOutcome is the bar a package outcome paints as.
func fromOutcome(out uistatusbar.Outcome) errBar {
	return errBar{text: out.Text(), done: out.Done(), warn: out.Warn(), shown: out.Shown(), age: out.Age()}
}

// reportErr puts a failure on the status bar; the poll ages it out.
func (m *Model) reportErr(text string) { m.errBar = fromOutcome(uistatusbar.Failed(text)) }

// clearErr takes whatever message is on the status bar down.
func (m *Model) clearErr() { m.errBar = errBar{} }

// reportDone puts an action that went through on the status bar.
func (m *Model) reportDone(text string) { m.errBar = fromOutcome(uistatusbar.Succeeded(text)) }

// reportWarn puts an action that went through with a caveat on the status
// bar.
func (m *Model) reportWarn(text string) { m.errBar = fromOutcome(uistatusbar.Warned(text)) }

type errMsg struct{ err error }

// ageError clears a status message after it has survived a couple of poll
// ticks, so transient errors self-dismiss without any per-callsite timers.
func (m *Model) ageError() {
	m.errBar = fromOutcome(m.errBar.outcome().Ticked())
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
	return uistatusbar.Toast(m.statusLine(), m.layout.width-leftWidth-2, uistatusbar.TonesFor(uitheme.Current()))
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
