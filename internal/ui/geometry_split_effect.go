package ui

import (
	"fmt"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

type splitSaveRequest struct {
	value string
}

func (splitSaveRequest) EffectRequest() {}

type splitSaveResult struct{}

func (splitSaveResult) EffectResult() {}

func (s effectServices) runSplitSave(request splitSaveRequest) (uieffects.Result, error) {
	if s.store == nil {
		return splitSaveResult{}, fmt.Errorf("save split ratio: store is unavailable")
	}
	if err := s.store.SetSetting(splitRatioSetting, request.value); err != nil {
		return splitSaveResult{}, fmt.Errorf("save split ratio: %w", err)
	}
	return splitSaveResult{}, nil
}

func (m *Model) applySplitSave(job *uieffects.Job, err error) {
	if job.ID != m.layout.split.saveID {
		return
	}
	if err == nil {
		if m.errBar.text == m.layout.split.saveError {
			m.clearErr()
		}
		m.layout.split.saveError = ""
		return
	}
	m.layout.split.saveError = err.Error()
	m.reportErr(m.layout.split.saveError)
}
