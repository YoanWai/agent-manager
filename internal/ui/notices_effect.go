package ui

import (
	"fmt"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// noticeDismissRequest persists one optimistic dismissal through the shared
// FIFO effect lane. The foreground fence reopens the failed notice only when
// the user has not moved on since accepting the dismissal.
type noticeDismissRequest struct {
	id            string
	foregroundGen uint64
	modal         bool
}

func (noticeDismissRequest) EffectRequest() {}

func (request noticeDismissRequest) copied() effectRequest { return request }

func (noticeDismissRequest) effectName() string { return "notice dismissal" }

func (request noticeDismissRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runNoticeDismiss(request)
}

type noticeDismissResult struct{}

func (noticeDismissResult) EffectResult() {}

func (noticeDismissResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applyNoticeDismiss(job, err)
	return nil
}

func (m *Model) noticeDismissQueued(id string) bool {
	queued := func(job *uieffects.Job) bool {
		if job == nil {
			return false
		}
		request, ok := job.Request.(noticeDismissRequest)
		return ok && request.id == id
	}
	if queued(m.effects.Main.Active) {
		return true
	}
	for _, job := range m.effects.Main.Pending {
		if queued(job) {
			return true
		}
	}
	return false
}

func (s effectServices) runNoticeDismiss(request noticeDismissRequest) (uieffects.Result, error) {
	if s.store == nil {
		return noticeDismissResult{}, fmt.Errorf("dismiss notice %q: store is unavailable", request.id)
	}
	if err := s.store.DismissNotice(request.id); err != nil {
		return noticeDismissResult{}, fmt.Errorf("dismiss notice %q: %w", request.id, err)
	}
	return noticeDismissResult{}, nil
}

func (m *Model) applyNoticeDismiss(job *uieffects.Job, err error) {
	request := job.Request.(noticeDismissRequest)
	if err == nil {
		return
	}
	m.notices.Restore(request.id)
	m.reportErr(err.Error())
	if request.modal && !m.effects.Quitting && request.foregroundGen == m.gens.foreground && (m.noticesOpen() || m.listClear()) {
		m.openNotices(request.id)
	}
}
