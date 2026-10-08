package ui

import "fmt"

// noticeDismissRequest persists one optimistic dismissal through the shared
// FIFO effect lane. The foreground fence reopens the failed notice only when
// the user has not moved on since accepting the dismissal.
type noticeDismissRequest struct {
	id            string
	foregroundGen uint64
	modal         bool
}

func (noticeDismissRequest) effectRequest() {}

type noticeDismissResult struct{}

func (noticeDismissResult) effectResult() {}

func (m *Model) noticeDismissQueued(id string) bool {
	queued := func(job *effectJob) bool {
		if job == nil {
			return false
		}
		request, ok := job.request.(noticeDismissRequest)
		return ok && request.id == id
	}
	if queued(m.effects.active) {
		return true
	}
	for _, job := range m.effects.pending {
		if queued(job) {
			return true
		}
	}
	return false
}

func (s effectServices) runNoticeDismiss(request noticeDismissRequest) (effectResult, error) {
	if s.store == nil {
		return noticeDismissResult{}, fmt.Errorf("dismiss notice %q: store is unavailable", request.id)
	}
	if err := s.store.DismissNotice(request.id); err != nil {
		return noticeDismissResult{}, fmt.Errorf("dismiss notice %q: %w", request.id, err)
	}
	return noticeDismissResult{}, nil
}

func (m *Model) applyNoticeDismiss(job *effectJob, err error) {
	request := job.request.(noticeDismissRequest)
	if err == nil {
		return
	}
	delete(m.notices.dismissed, request.id)
	m.reportErr(err.Error())
	if request.modal && !m.effects.quitting && request.foregroundGen == m.gens.foreground && (m.mode == modeList || m.mode == modeNotices) {
		m.openNotices(request.id)
	}
}
