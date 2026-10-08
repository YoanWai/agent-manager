package ui

import "fmt"

type splitSaveRequest struct {
	value string
}

func (splitSaveRequest) effectRequest() {}

type splitSaveResult struct{}

func (splitSaveResult) effectResult() {}

func (s effectServices) runSplitSave(request splitSaveRequest) (effectResult, error) {
	if s.store == nil {
		return splitSaveResult{}, fmt.Errorf("save split ratio: store is unavailable")
	}
	if err := s.store.SetSetting(splitRatioSetting, request.value); err != nil {
		return splitSaveResult{}, fmt.Errorf("save split ratio: %w", err)
	}
	return splitSaveResult{}, nil
}

func (m *Model) applySplitSave(job *effectJob, err error) {
	if job.id != m.layout.split.saveID {
		return
	}
	if err == nil {
		if m.errBar.text == m.layout.split.saveError {
			m.errBar.text = ""
		}
		m.layout.split.saveError = ""
		return
	}
	m.layout.split.saveError = err.Error()
	m.errBar.text = m.layout.split.saveError
}
