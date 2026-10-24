package panes

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// Services is what the geometry effects run against: the settings store and
// pane box a resize writes, the tmux server it pins, and the poller's run
// mutex.
type Services struct {
	Store    Store
	Driver   Driver
	Reflower Reflower
}

// ResizeRequest is the effect lane's pane resize: this package's Resize and
// nothing else.
type ResizeRequest struct {
	Resize
}

func (ResizeRequest) EffectName() string { return "pane resize" }

func (value ResizeRequest) EffectCopy() uieffects.Request {
	value.Resize = Resize{Publish: value.Publish, Targets: append([]PaneResize(nil), value.Targets...)}
	return value
}

func (request ResizeRequest) Run(services Services) (uieffects.Result, error) {
	result, err := Run(services.Store, services.Driver, services.Reflower, request.Resize)
	return ResizeResult{Result: result}, err
}

// ResizeResult is what a resize landed, for the pane cache to record.
type ResizeResult struct{ Result }

func (ResizeResult) EffectResult() {}

// SplitSaveRequest persists a committed divider ratio.
type SplitSaveRequest struct {
	Value string
}

func (SplitSaveRequest) EffectName() string { return "split ratio save" }

func (request SplitSaveRequest) EffectCopy() uieffects.Request { return request }

func (request SplitSaveRequest) Run(services Services) (uieffects.Result, error) {
	if services.Store == nil {
		return SplitSaveResult{}, NoStore
	}
	return SplitSaveResult{}, SaveRatio(services.Store, request.Value)
}

// SplitSaveResult carries nothing back: the job that failed owns the status
// bar through its id.
type SplitSaveResult struct{}

func (SplitSaveResult) EffectResult() {}
