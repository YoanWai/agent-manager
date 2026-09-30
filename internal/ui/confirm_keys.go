package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The card advertises y/enter and n/esc; anything else leaves it up rather
	// than dismissing the question the user has not answered.
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "enter", "n", "esc":
	default:
		return m, nil
	}
	// A relaunch the manager refused opened the hint dialog; every other
	// answer falls back to the list.
	defer func() {
		if m.mode != modeLaunchHint {
			m.mode = modeList
		}
	}()
	switch msg.String() {
	case "y", "enter":
		switch m.confirm.action {
		case actionArchive:
			if err := m.archiveConfirmed(); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
			m.errBar.text = ""
		case actionRestore:
			if err := m.restoreConfirmed(); err != nil {
				m.reportLaunchError(err, m.restoreConfirmed)
				return m, nil
			}
		case actionKill:
			for _, sess := range m.confirm.sessions {
				if err := m.killSession(sess); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
			}
			m.errBar.text = ""
			m.rebuildRows()
		case actionRestart:
			for _, sess := range m.confirm.sessions {
				if err := m.restartSession(sess); err != nil {
					m.reportLaunchError(err, func() error { return m.restartSession(sess) })
					return m, nil
				}
			}
			m.errBar.text = ""
			m.rebuildRows()
		case actionRevive:
			if m.confirm.batch {
				panes, err := m.services.tmux.Panes()
				if err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				var stillDead []store.Session
				for _, sess := range m.confirm.sessions {
					if panes[sess.ID].PID == 0 {
						stillDead = append(stillDead, sess)
					}
				}
				m.confirm = confirmTarget{}
				return m.reviveMany(stillDead, "")
			}
			for _, sess := range m.confirm.sessions {
				if m.services.tmux.Exists(sess.ID) {
					continue
				}
				if err := m.reviveSession(sess); err != nil {
					m.reportLaunchError(err, func() error { return m.reviveSession(sess) })
					return m, nil
				}
			}
			m.errBar.text = ""
		case actionDelete:
			if _, err := m.deleteConfirmed(); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
		default:
			m.errBar.text = fmt.Sprintf("unknown confirm action %q", m.confirm.action)
			return m, nil
		}
		m.confirm = confirmTarget{}
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}
