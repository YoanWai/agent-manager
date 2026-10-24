package ui

import (
	"time"

	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

// endAfterTurns archives or kills each session whose turn ended after it
// asked for that. Dispatch is enqueue-only: Update's trailing nextEffectCmd
// starts it.
func (m *Model) endAfterTurns(ids []string) {
	for _, id := range ids {
		m.enqueueEffect(uilifecycle.AfterTurnRequest{ID: id, Watched: m.focus.runtime.Watching()}, 0, false)
	}
}

// cancelEndSelected keeps the selected session past its turn, withdrawing the
// archive or kill it asked for.
func (m *Model) cancelEndSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || entry.sess.AfterTurn == "" {
		return m, nil
	}
	m.enqueueEffect(uilifecycle.AfterTurnRequest{ID: entry.sess.ID, Name: m.displayName(entry.sess), Cancel: true}, 0, false)
	return m, nil
}

func (m *Model) applyAfterTurn(request uilifecycle.AfterTurnRequest, result uilifecycle.AfterTurnResult, err error) tea.Cmd {
	var command tea.Cmd
	if result.Target.Action != uilifecycle.None {
		command = m.applyLifecycleEffect(uilifecycle.Request{Target: result.Target, Watched: request.Watched}, result.Lifecycle, err)
	} else {
		if err != nil {
			m.reportErr(err.Error())
		}
		m.rewatchSurvivor(request.Watched, result.Lifecycle.Survivor)
	}
	if result.Cleared {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == request.ID {
				m.workspace.sessions[i].AfterTurn, m.workspace.sessions[i].AfterTurnAt = "", time.Time{}
			}
		}
		m.rebuildRows()
		if request.Cancel {
			m.reportDone(request.Name + " stays once its turn ends")
		}
	}
	return command
}
