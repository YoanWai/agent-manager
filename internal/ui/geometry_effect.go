package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	tea "github.com/charmbracelet/bubbletea"
)

// geometryRequest is the effect lane's pane resize: it carries the package's
// Resize and adds nothing.
type geometryRequest struct {
	uigeometrypanes.Resize
}

func (geometryRequest) EffectRequest() {}

func (geometryRequest) effectName() string { return "pane resize" }

func (value geometryRequest) copied() effectRequest {
	value.Resize = uigeometrypanes.Resize{Publish: value.Publish, Targets: append([]uigeometrypanes.PaneResize(nil), value.Targets...)}
	return value
}

func (request geometryRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runGeometry(request.Resize)
}

type geometryEffectResult struct{ uigeometrypanes.Result }

func (geometryEffectResult) EffectResult() {}

func (result geometryEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applyGeometryEffect(result.Result, err)
	return nil
}

// queueGeometry queues a resize, dropping what the lane already carries and
// folding into the geometry job queued just before it, never across an
// accepted mutation.
func (m *Model) queueGeometry(request uigeometrypanes.Resize) {
	request = uigeometrypanes.Effective(request, m.queuedGeometry())
	if request.Empty() {
		return
	}
	if n := len(m.effects.Main.Pending); n > 0 {
		if previous, ok := m.effects.Main.Pending[n-1].Request.(geometryRequest); ok {
			request = uigeometrypanes.Merge(previous.Resize, request)
			m.effects.Main.Pending = m.effects.Main.Pending[:n-1]
		}
	}
	m.enqueueEffect(geometryRequest{Resize: request}, 0, false)
}

// queuedGeometry reads the resizes already waiting on the lane, in the order
// the lane will run them.
func (m *Model) queuedGeometry() []uigeometrypanes.Resize {
	jobs := append([]*uieffects.Job(nil), m.effects.Main.Pending...)
	if m.effects.Main.Active != nil {
		jobs = append([]*uieffects.Job{m.effects.Main.Active}, jobs...)
	}
	queued := make([]uigeometrypanes.Resize, 0, len(jobs))
	for _, job := range jobs {
		if request, ok := job.Request.(geometryRequest); ok {
			queued = append(queued, request.Resize)
		}
	}
	return queued
}

func (s effectServices) runGeometry(request uigeometrypanes.Resize) (uieffects.Result, error) {
	var reflower uigeometrypanes.Reflower
	if s.runner != nil {
		reflower = s.runner
	}
	result, err := uigeometrypanes.Run(s.store, s.driver, reflower, request)
	return geometryEffectResult{Result: result}, err
}

func (m *Model) applyGeometryEffect(result uigeometrypanes.Result, err error) {
	m.layout.panes.Apply(result)
	if err != nil {
		m.reportErr(err.Error())
	}
}
