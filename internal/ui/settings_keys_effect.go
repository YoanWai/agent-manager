package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

type keysRequest struct {
	list, session  keybind.Table
	listChanged    bool
	sessionChanged bool
}

func (keysRequest) EffectRequest() {}

func (request keysRequest) copied() effectRequest { return request }

func (keysRequest) effectName() string { return "key binding save" }

func (request keysRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runKeys(request)
}

type keysEffectResult struct {
	listCommitted    bool
	sessionCommitted bool
}

func (keysEffectResult) EffectResult() {}

func (result keysEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyKeysEffect(job, result, err)
}

func (s effectServices) runKeys(request keysRequest) (uieffects.Result, error) {
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

func (m *Model) applyKeysEffect(job *uieffects.Job, result keysEffectResult, err error) tea.Cmd {
	request := job.Request.(keysRequest)
	if result.listCommitted {
		m.services.listKeys = request.list
	}
	if result.sessionCommitted {
		m.services.keys = request.session
		m.services.tmux.SetSessionKeys(request.session)
	}
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if result.sessionCommitted {
		return m.refreshExistingSessionUX
	}
	return nil
}
