package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.handleFormKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	case modeHelp:
		m.mode = modeList
		return m, nil
	}

	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "enter":
		return m.attachSelected()
	case "n":
		m.openForm()
	case "a":
		return m.archiveSelected()
	case "u":
		return m.restoreSelected()
	case "d":
		if _, ok := m.selected(); ok {
			m.mode = modeConfirmDelete
		}
	case "space":
		m.toggleCollapse()
	case "t":
		m.showArchived = !m.showArchived
		return m, m.refreshCmd()
	case "/":
		m.searching = true
		m.err = ""
	case "r":
		return m, m.refreshCmd()
	case "?":
		m.mode = modeHelp
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.nav) {
		m.cursor = len(m.nav) - 1
	}
}

func (m *Model) toggleCollapse() {
	sess, ok := m.selected()
	if !ok {
		return
	}
	m.collapsed[sess.Group] = !m.collapsed[sess.Group]
	m.rebuildNav()
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.err = "session is not running (dead); delete or recreate it"
		return m, nil
	}
	m.err = ""
	cmd := m.tmux.AttachCommand(sess.ID)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return attachDoneMsg{err}
	})
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if err := m.store.SetArchived(sess.ID, true); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, m.refreshCmd()
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if err := m.store.SetArchived(sess.ID, false); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, m.refreshCmd()
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		sess, ok := m.selected()
		m.mode = modeList
		if !ok {
			return m, nil
		}
		if err := m.tmux.Kill(sess.ID); err != nil {
			m.err = err.Error()
			return m, nil
		}
		if err := m.store.Delete(sess.ID); err != nil {
			m.err = err.Error()
			return m, nil
		}
		return m, m.refreshCmd()
	default:
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.searching = false
	case "backspace":
		if len(m.search) > 0 {
			m.search = m.search[:len(m.search)-1]
		}
		m.rebuildNav()
	default:
		if len(msg.String()) == 1 {
			m.search += msg.String()
			m.rebuildNav()
		}
	}
	return m, nil
}
