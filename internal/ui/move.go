package ui

import tea "github.com/charmbracelet/bubbletea"

func (m *Model) openMove() {
	sess, ok := m.selected()
	if !ok {
		return
	}
	m.moveID = sess.ID
	m.rebuildGroupOptions(sess.Group)
	m.mode = modeMove
	m.errBar.text = ""
}

func (m *Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "up":
		m.moveGroupCursor(-1)
		return m, nil
	case "down":
		m.moveGroupCursor(1)
		return m, nil
	case "enter":
		group := m.selectedGroupPath()
		if err := m.store.MoveSession(m.moveID, group); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.relabelSession(m.moveID)
		m.mode = modeList
		m.requestRefresh()
		return m, nil
	}
	return m, nil
}
