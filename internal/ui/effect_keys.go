package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

type keysRequest struct {
	list, session  keybind.Table
	listChanged    bool
	sessionChanged bool
}

func (keysRequest) effectRequest() {}

type keysEffectResult struct {
	listCommitted    bool
	sessionCommitted bool
}

func (keysEffectResult) effectResult() {}

func (s effectServices) runKeys(request keysRequest) (effectResult, error) {
	result := keysEffectResult{}
	tot := 0
	if request.listChanged {
		tot++
	}
	if request.sessionChanged {
		tot++
	}
	committed := 0
	if request.listChanged {
		if err := s.saveKeys(request.list); err != nil {
			return result, fmt.Errorf("key save committed %d of %d writes: %w", committed, tot, err)
		}
		result.listCommitted = true
		committed++
	}
	if request.sessionChanged {
		if err := s.saveKeys(request.session); err != nil {
			return result, fmt.Errorf("key save committed %d of %d writes: %w", committed, tot, err)
		}
		result.sessionCommitted = true
	}
	return result, nil
}

func (m *Model) applyKeysEffect(job *effectJob, result keysEffectResult, err error) tea.Cmd {
	request := job.request.(keysRequest)
	if result.listCommitted {
		m.services.listKeys = request.list
	}
	if result.sessionCommitted {
		m.services.keys = request.session
		m.services.tmux.SetSessionKeys(request.session)
	}
	if err != nil {
		m.errBar.text = err.Error()
		return nil
	}
	if result.sessionCommitted {
		return m.refreshExistingSessionUX
	}
	return nil
}
