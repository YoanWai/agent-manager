package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// killSelected asks to end the selected session, or every live session
// under the selected group, freeing the RAM their agents hold while the
// rows stay put for v to revive.
func (m *Model) killSelected() (tea.Model, tea.Cmd) {
	if m.rail.ShowArchived() {
		return m, nil
	}
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		live := m.liveSessions(m.sessionsInGroup(entry.group))
		if len(live) == 0 {
			m.reportErr("no live sessions to kill in " + displayGroup(entry.group))
			return m, nil
		}
		m.confirm.confirmTarget = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionKill,
			sessions: live,
			label: fmt.Sprintf("kill group %s (%d live sessions)? frees their RAM, v revives them.",
				displayGroup(entry.group), len(live)),
		}
	} else {
		members := m.sessionAndChildren(entry.sess)
		sessions := m.liveSessions(members)
		if len(sessions) == 0 {
			m.reportErr(entry.sess.Name + " is already dead")
			return m, nil
		}
		m.confirm.confirmTarget = confirmTarget{
			action:   actionKill,
			sessions: sessions,
			label: sessionActionLabel("kill", entry.sess, sessions,
				"frees its RAM, v revives it.",
				"frees their RAM, v revives them."),
			selection: lifecycleSelection{kind: lifecycleSelectionSession, rootID: entry.sess.ID, members: members},
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// killAllLive asks to end every live session in the current view, the
// batch counterpart to V.
func (m *Model) killAllLive() (tea.Model, tea.Cmd) {
	if m.rail.ShowArchived() {
		return m, nil
	}
	live := m.liveSessions(m.listedSessions())
	if len(live) == 0 {
		m.reportErr("no live sessions to kill")
		return m, nil
	}
	m.confirm.confirmTarget = confirmTarget{
		action:   actionKill,
		sessions: live,
		label:    fmt.Sprintf("kill every live session (%d)? frees their RAM, v revives them.", len(live)),
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// liveSessions narrows a list using the pane projection from the last poll.
// The lifecycle worker checks the live driver again before mutating it.
func (m *Model) liveSessions(sessions []store.Session) []store.Session {
	var live []store.Session
	for _, sess := range sessions {
		if m.sessionWindowProjectedLive(sess.ID) {
			live = append(live, sess)
		}
	}
	return live
}

// sessionWindowProjectedLive combines the last poll with launches accepted
// since that poll. The lifecycle worker checks tmux again before it mutates;
// this projection only decides which confirmation the UI presents.
func (m *Model) sessionWindowProjectedLive(id string) bool {
	if m.workspace.panes[id].PID > 0 {
		return true
	}
	_, launched := m.ledger.launched[id]
	return launched
}

// killSession ends one session's tmux window, freeing everything its agent
// held, while the store row keeps the name, group, history and conversation
// id that revive needs. The pane is captured first so the preview still
// shows the agent's last output once the window is gone.

func (m *Model) watchedSession() string {
	if m.focus.runtime.watch == nil {
		return ""
	}
	return m.focus.runtime.watch.Watching()
}

func sessionActionLabel(verb string, root store.Session, sessions []store.Session, one, many string) string {
	for _, session := range sessions {
		if session.ID == root.ID {
			return followConfirmLabel(verb, root.Name, len(sessions)-1, one, many)
		}
	}
	unit, suffix := "terminal", one
	if len(sessions) != 1 {
		unit, suffix = "terminals", many
	}
	return fmt.Sprintf("%s %d %s of %s? %s", verb, len(sessions), unit, root.Name, suffix)
}
