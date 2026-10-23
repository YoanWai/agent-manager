package ui

import tea "github.com/charmbracelet/bubbletea"

type errBar struct {
	text  string
	done  string
	warn  string
	shown string
	age   int
}

// worked reports whether the message on the bar is an action that went
// through, so it can be styled as an outcome rather than a failure. Only
// reportDone fills done, and any later write to text alone leaves it
// behind, so a message says it worked or reads as a failure.
func (e errBar) worked() bool { return e.text != "" && e.text == e.done }

// warned reports whether the message is an action that went through with
// a caveat the reader has to see, which reads as neither outcome nor
// failure.
func (e errBar) warned() bool { return e.text != "" && e.text == e.warn }

// reportErr puts a failure on the status bar; the poll ages it out.
func (m *Model) reportErr(text string) {
	m.errBar.text = text
}

// clearErr takes whatever message is on the status bar down.
func (m *Model) clearErr() {
	m.errBar.text = ""
}

// reportDone puts an action that went through on the status bar.
func (m *Model) reportDone(text string) {
	m.errBar.text, m.errBar.done = text, text
}

// reportWarn puts an action that went through with a caveat on the status
// bar.
func (m *Model) reportWarn(text string) {
	m.errBar.text, m.errBar.warn = text, text
}

type errMsg struct{ err error }

// ageError clears a status message after it has survived a couple of poll
// ticks, so transient errors self-dismiss without any per-callsite timers.
func (m *Model) ageError() {
	if m.errBar.text == "" {
		m.errBar = errBar{}
		return
	}
	if m.errBar.text != m.errBar.shown {
		m.errBar.shown, m.errBar.age = m.errBar.text, 0
		return
	}
	m.errBar.age++
	if m.errBar.age >= 2 {
		m.errBar = errBar{}
	}
}

func (m *Model) routeStatusMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case errMsg:
		m.reportErr(msg.err.Error())
		return routed(m, nil)
	}
	return nil, nil, false
}
