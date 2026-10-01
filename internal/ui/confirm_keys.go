package ui

import tea "github.com/charmbracelet/bubbletea"

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "n", "esc":
		m.confirm = confirmTarget{}
		m.mode = modeList
		return m, nil
	case "y", "enter":
		target := m.confirm
		target.sessions = append(target.sessions[:0:0], target.sessions...)
		m.confirm = confirmTarget{}
		m.mode = modeList
		m.queueLifecycle(target, false, "")
		return m, m.nextEffectCmd()
	}
	return m, nil
}
