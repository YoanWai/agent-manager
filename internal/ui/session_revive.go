package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

// deadSessionHint names both ways back from a dead row: revive resumes the
// conversation it held, restart drops it.
const deadSessionHint = "session is dead - press v to revive or R to restart"

// reviveSelected relaunches a dead session's tmux session under the same
// id, keeping its name, group, and history. Tools with a revive_command
// resume where they left off (e.g. claude --continue). On a group row it
// revives the whole subtree, mirroring the group kill.
func (m *Model) reviveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		sessions := m.sessionsInGroup(entry.group)
		if dead := deadSessions(sessions); len(dead) > 1 {
			m.confirm = confirmTarget{
				isGroup:  true,
				path:     entry.group,
				action:   actionRevive,
				batch:    true,
				sessions: dead,
				label: fmt.Sprintf("revive group %s (%d dead sessions)? brings them back.",
					displayGroup(entry.group), len(dead)),
			}
			m.mode = modeConfirmDelete
			return m, nil
		}
		return m.reviveMany(sessions, "no dead sessions to revive in "+entry.group)
	}
	set, err := m.sessionAndChildren(entry.sess)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	dead := false
	for _, sess := range set {
		if !m.services.tmux.Exists(sess.ID) {
			dead = true
			break
		}
	}
	if len(set) > 1 && dead {
		m.confirm = confirmTarget{
			action:   actionRevive,
			sessions: set,
			label: followConfirmLabel("revive", entry.sess.Name, len(set)-1,
				"brings it back.",
				"brings them back."),
		}
		m.mode = modeConfirmDelete
		return m, nil
	}
	m.queueLifecycle(confirmTarget{action: actionRevive, sessions: []store.Session{entry.sess}}, true, "")
	m.errBar.text = m.degradedResumeNotice(entry.sess)
	return m, m.nextEffectCmd()
}

// reviveAllDead relaunches every dead session in the current view, resuming
// each by its captured id where one exists.
func (m *Model) reviveAllDead() (tea.Model, tea.Cmd) {
	sessions := m.listedSessions()
	if dead := deadSessions(sessions); len(dead) > 1 {
		m.confirm = confirmTarget{
			action:   actionRevive,
			batch:    true,
			sessions: dead,
			label:    fmt.Sprintf("revive every dead session (%d)? brings them back.", len(dead)),
		}
		m.mode = modeConfirmDelete
		return m, nil
	}
	return m.reviveMany(sessions, "no dead sessions to revive")
}

// reviveMany relaunches every dead session in the list. It revives what it
// can and names the first failure rather than stopping, so one broken
// session does not block the rest.
func (m *Model) reviveMany(sessions []store.Session, emptyNotice string) (tea.Model, tea.Cmd) {
	m.queueLifecycle(confirmTarget{action: actionRevive, batch: true, sessions: deadSessions(sessions)}, false, emptyNotice)
	return m, m.nextEffectCmd()
}

func deadSessions(sessions []store.Session) []store.Session {
	var dead []store.Session
	for _, sess := range sessions {
		if sess.Status == status.Dead {
			dead = append(dead, sess)
		}
	}
	return dead
}

// sessionsInGroup lists the sessions the current view shows at or below a
// group, so a group action covers exactly the rows under it on screen.
func (m *Model) sessionsInGroup(path string) []store.Session {
	var sessions []store.Session
	for _, sess := range m.listedSessions() {
		if inGroupSubtree(sess.Group, path) {
			sessions = append(sessions, sess)
		}
	}
	return sessions
}

// degradedResumeNotice warns when a revived session had to fall back to the
// working directory's most recent conversation because its own conversation
// id was never captured, which resumes the wrong conversation whenever
// sessions share a directory.
func (m *Model) degradedResumeNotice(sess store.Session) string {
	tool, ok := m.services.cfg.Tools[sess.Tool]
	if !ok || sess.AgentSessionID != "" || tool.ResumeByIDCommand == "" || tool.ResumePickerCommand != "" {
		return ""
	}
	return fmt.Sprintf("revived %s with --continue: no conversation id captured, may resume the wrong conversation", sess.Name)
}

// reviveSession relaunches one dead session under its old id, keeping its
// name, group, and history. When the session's own conversation id was
// captured, it resumes that exact conversation via the tool's
// resume_by_id_command instead of the working directory's most recent one,
// which would be the wrong conversation whenever sessions share a cwd.

// bindReviveLocally mirrors the store write in the loaded rows, so the
// startup loader can paint before the next poll re-reads them.
func (m *Model) bindReviveLocally(id string, launchedAt time.Time) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID != id {
			continue
		}
		m.workspace.sessions[i].AgentLaunchedAt = launchedAt
		m.workspace.sessions[i].Status = status.Starting
		m.workspace.sessions[i].Acked = false
	}
	if sel, ok := m.selected(); ok && sel.ID == id {
		// A killed or archived row still holds its last pane; that would
		// count as painted and hide the loader.
		m.workspace.preview = ""
	}
}
