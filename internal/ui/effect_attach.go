package ui

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
)

type attachRequest struct {
	id         string
	reattach   bool
	generation int
}

func (attachRequest) effectRequest() {}

type attachEffectResult struct {
	warning error
	fatal   error
}

func (attachEffectResult) effectResult() {}
func (s effectServices) runAttach(request attachRequest) (effectResult, error) {
	result := attachEffectResult{}
	err := s.reflow([]string{request.id}, func() {
		if !s.driver.Exists(request.id) {
			result.fatal = errors.New(deadSessionHint)
			return
		}
		sess, readErr := s.store.Get(request.id)
		if readErr != nil {
			result.fatal = readErr
			return
		}
		if sess.Status == status.Finished {
			if ackErr := s.store.AcknowledgeFinished(request.id); ackErr != nil {
				result.fatal = ackErr
				return
			}
		}
		result.warning = s.driver.PrepareAttach(request.id)
	})
	if err != nil {
		return result, err
	}
	return result, result.fatal
}
func (m *Model) applyAttachEffect(request attachRequest, result attachEffectResult, err error) tea.Cmd {
	if err != nil {
		m.errBar.text = err.Error()
		return nil
	}
	if request.reattach && (request.generation != m.review.Generation() || m.review.Active()) {
		return nil
	}
	if m.effects.quitting {
		return nil
	}
	if result.warning != nil {
		m.errBar.text = result.warning.Error()
	}
	return execTerminalProcess(m.services.tmux.AttachCommand(request.id), func(err error) tea.Msg { return attachDoneMsg{sessID: request.id, err: err} })
}
