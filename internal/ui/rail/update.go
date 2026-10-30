package rail

import tea "github.com/charmbracelet/bubbletea"

// Update is the Rail's own slice of a key press: it tells the Rail the key
// and returns the decision for the root to run, with whether the press was
// the Rail's.
func (m *Model) Update(msg tea.KeyMsg, frame Frame, ctx KeyContext) (Decision, bool) {
	busy := m.Busy()
	decision := m.Key(msg, frame, ctx)
	return decision, decision.Consumed || busy
}
