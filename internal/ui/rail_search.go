package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.rail.searching = false
	case "esc":
		m.rail.searching = false
		return m, m.clearSearch()
	case "backspace":
		if len(m.rail.search) > 0 {
			m.rail.search = m.rail.search[:len(m.rail.search)-1]
		}
		m.rebuildRows()
	default:
		if len(msg.String()) == 1 {
			m.rail.search += msg.String()
			m.rebuildRows()
		}
	}
	return m, nil
}

// clearSearch drops the query and re-lists. A query that outlives its field
// with no way back is what makes filtered-away sessions read as sessions
// that are gone, so esc answers from the list as well as from the field.
func (m *Model) clearSearch() tea.Cmd {
	if m.rail.search == "" {
		return nil
	}
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	m.rail.search = ""
	m.rebuildRows()
	return m.afterListFilter(previousKey)
}
