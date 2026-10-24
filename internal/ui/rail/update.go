package rail

import tea "github.com/charmbracelet/bubbletea"

// Update is the Rail's own slice of a key press: it tells the Rail the key,
// consumes every decision that changes only the Rail, and returns the ones
// the root has to run, since a cursor move re-aims the quick bar, a filter
// resyncs the poll input, and a mutation has to reach the store.
func (m *Model) Update(msg tea.KeyMsg, frame Frame, ctx KeyContext) (Decision, bool) {
	busy := m.Busy()
	decision := m.Key(msg, frame, ctx)
	if (decision.Consumed || busy) && queued(decision) {
		return Decision{}, true
	}
	return decision, decision.Consumed || busy
}

// queued reports whether the decision changes something root owns: the
// selection's side effects, a stored mutation, a refresh or an action.
func queued(decision Decision) bool {
	return decision.SelectionEffect != SelectionEffectNone ||
		len(decision.Mutations) > 0 ||
		decision.Refresh ||
		decision.AutoScroll != nil ||
		decision.Intent.Kind != NoAction
}
