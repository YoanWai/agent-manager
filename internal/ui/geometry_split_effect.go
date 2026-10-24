package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	tea "github.com/charmbracelet/bubbletea"
)

type splitSaveRequest struct {
	value string
}

func (splitSaveRequest) EffectRequest() {}

func (request splitSaveRequest) copied() effectRequest { return request }

func (splitSaveRequest) effectName() string { return "split ratio save" }

func (request splitSaveRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runSplitSave(request)
}

type splitSaveResult struct{}

func (splitSaveResult) EffectResult() {}

func (splitSaveResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.layout.panes.ApplySplit(paneSizing{m}, job.ID, err)
	return nil
}

func (s effectServices) runSplitSave(request splitSaveRequest) (uieffects.Result, error) {
	if s.store == nil {
		return splitSaveResult{}, uigeometrypanes.NoStore
	}
	return splitSaveResult{}, uigeometrypanes.SaveRatio(s.store, request.value)
}
