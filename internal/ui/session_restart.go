package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"time"
)

// restartSelected asks to relaunch the selected session with an empty
// context: the same row, directory and tool, running a brand new
// conversation instead of resuming the one it held.
func (m *Model) restartSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		m.reportErr("restart applies to a session; pick one under " + displayGroup(entry.group))
		return m, nil
	}
	label := fmt.Sprintf("restart %s with an empty context? its current conversation is left behind.", entry.sess.Name)
	if m.sessionWindowProjectedLive(entry.sess.ID) {
		label = fmt.Sprintf("restart %s with an empty context? ends the running agent and leaves its conversation behind.", entry.sess.Name)
	}
	m.confirm = confirmTarget{
		action:   actionRestart,
		sessions: []store.Session{entry.sess},
		label:    label,
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// restartSession relaunches a session's tool from scratch. The conversation
// it was resuming is retired rather than resumed, so the agent comes back
// with the same name, directory and group but no context to carry.

// restartLaunch builds what a restart runs: the tool's plain launch command,
// exactly as a brand new session gets it, plus a fresh conversation id for
// the tools that take one. Tools that mint their own id instead get nothing
// to carry, and the poller captures what they wrote.
func restartLaunch(tool config.Tool) (baseCommand, agentSessionID string) {
	if tool.SessionIDFlag == "" {
		return tool.Command, ""
	}
	agentSessionID = uuid.NewString()
	return tool.Command + " " + tool.SessionIDFlag + " " + agentSessionID, agentSessionID
}

// bindRestartLocally mirrors the store write in the loaded rows, so the list
// redraws on the new conversation before the next poll re-reads it.
func (m *Model) bindRestartLocally(id, agentSessionID string, launchedAt time.Time) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID != id {
			continue
		}
		if m.workspace.sessions[i].AgentSessionID != "" {
			m.workspace.sessions[i].RetiredAgentSessionID = m.workspace.sessions[i].AgentSessionID
		}
		m.workspace.sessions[i].AgentSessionID = agentSessionID
		m.workspace.sessions[i].AgentLaunchedAt = launchedAt
		m.workspace.sessions[i].Status = status.Starting
		m.workspace.sessions[i].Acked = false
	}
}
