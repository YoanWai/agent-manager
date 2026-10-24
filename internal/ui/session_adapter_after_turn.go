package ui

import (
	"time"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

// afterTurnRequest carries out, or withdraws, the archive or kill a session
// asked for once its turn ends. The run rereads the row, because the poll
// report that queued it can trail a newer turn or an earlier pass's work.
type afterTurnRequest struct {
	id      string
	name    string
	cancel  bool
	watched string
}

func (afterTurnRequest) EffectRequest() {}

func (request afterTurnRequest) copied() effectRequest { return request }

func (afterTurnRequest) effectName() string { return "after-turn archive or kill" }

func (request afterTurnRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runAfterTurn(request)
}

type afterTurnResult struct {
	uilifecycle.AfterTurnResult
}

func (afterTurnResult) EffectResult() {}

func (result afterTurnResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyAfterTurn(job.Request.(afterTurnRequest), result, err)
}

// endAfterTurns archives or kills each session whose turn ended after it
// asked for that. Dispatch is enqueue-only: Update's trailing nextEffectCmd
// starts it.
func (m *Model) endAfterTurns(ids []string) {
	for _, id := range ids {
		m.enqueueEffect(afterTurnRequest{id: id, watched: m.focus.runtime.Watching()}, 0, false)
	}
}

// cancelEndSelected keeps the selected session past its turn, withdrawing the
// archive or kill it asked for.
func (m *Model) cancelEndSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || entry.sess.AfterTurn == "" {
		return m, nil
	}
	m.enqueueEffect(afterTurnRequest{id: entry.sess.ID, name: m.displayName(entry.sess), cancel: true}, 0, false)
	return m, nil
}

func (s effectServices) runAfterTurn(request afterTurnRequest) (uieffects.Result, error) {
	result, err := uilifecycle.RunAfterTurn(s.lifecycleServices(), uilifecycle.AfterTurnRequest{
		ID:      request.id,
		Name:    request.name,
		Cancel:  request.cancel,
		Watched: request.watched,
	})
	return afterTurnResult{result}, err
}

func (m *Model) applyAfterTurn(request afterTurnRequest, result afterTurnResult, err error) tea.Cmd {
	var command tea.Cmd
	if result.Target.Action != uilifecycle.None {
		command = m.applyLifecycleEffect(lifecycleRequest{target: confirmTargetFor(result.Target), watched: request.watched}, lifecycleEffectResult{result.Lifecycle}, err)
	} else {
		if err != nil {
			m.reportErr(err.Error())
		}
		m.rewatchSurvivor(request.watched, result.Lifecycle.Survivor)
	}
	if result.Cleared {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == request.id {
				m.workspace.sessions[i].AfterTurn, m.workspace.sessions[i].AfterTurnAt = "", time.Time{}
			}
		}
		m.rebuildRows()
		if request.cancel {
			m.reportDone(request.name + " stays once its turn ends")
		}
	}
	return command
}
