package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// afterTurnRequest carries out, or withdraws, the archive or kill a session
// asked for once its turn ends. Lifecycle.EndAfterTurn rereads the row,
// because the poll report that queued it can trail a newer turn or an
// earlier pass's work.
type afterTurnRequest struct {
	id      string
	name    string
	cancel  bool
	watched string
}

func (afterTurnRequest) effectRequest() {}

type afterTurnResult struct {
	target    confirmTarget
	lifecycle lifecycleEffectResult
	cleared   bool
}

func (afterTurnResult) effectResult() {}

// endAfterTurns archives or kills each session whose turn ended after it
// asked for that. Dispatch is enqueue-only: Update's trailing nextEffectCmd
// starts it.
func (m *Model) endAfterTurns(ids []string) {
	for _, id := range ids {
		m.enqueueEffect(afterTurnRequest{id: id, watched: m.watchedSession()}, 0, false)
	}
}

// cancelEndSelected keeps the selected session past its turn, withdrawing
// the archive or kill it asked for.
func (m *Model) cancelEndSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || entry.sess.AfterTurn == "" {
		return m, nil
	}
	m.enqueueEffect(afterTurnRequest{id: entry.sess.ID, name: m.displayName(entry.sess), cancel: true}, 0, false)
	return m, nil
}

func (s effectServices) runAfterTurn(request afterTurnRequest) (effectResult, error) {
	result := afterTurnResult{}
	if request.cancel {
		err := s.store.ClearAfterTurn(request.id)
		result.cleared = err == nil
		return result, err
	}
	children, err := s.store.Children(request.id)
	if err != nil {
		return result, err
	}
	ids := []string{request.id}
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	var ended sessioncmd.EndAfterTurnResult
	var endErr error
	reflowErr := s.reflow(ids, func() {
		// Stop watching first, so the watcher does not report a kill it asked for as a lost client.
		if s.watch != nil {
			for _, id := range ids {
				s.watch.unwatch(id)
			}
		}
		ended, endErr = s.lifecycle.EndAfterTurn(request.id)
		if request.watched != "" {
			result.lifecycle.survivor = s.driver.Exists(request.watched)
		}
	})
	if reflowErr != nil {
		return result, reflowErr
	}
	switch ended.Action {
	case store.AfterTurnArchive:
		result.target = confirmTarget{action: actionArchive, sessions: ended.Sessions}
		result.lifecycle.archive.Sessions = ended.Sessions
	case store.AfterTurnKill:
		result.target = confirmTarget{action: actionKill, sessions: ended.Sessions}
		result.lifecycle.changed = ended.Sessions
	}
	result.cleared = ended.Action != ""
	return result, endErr
}

func (m *Model) applyAfterTurn(request afterTurnRequest, result afterTurnResult, err error) tea.Cmd {
	var command tea.Cmd
	if result.target.action != "" {
		command = m.applyLifecycleEffect(lifecycleRequest{target: result.target, watched: request.watched}, result.lifecycle, err)
	} else {
		if err != nil {
			m.reportErr(err.Error())
		}
		m.rewatchSurvivor(request.watched, result.lifecycle.survivor)
	}
	if result.cleared {
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
