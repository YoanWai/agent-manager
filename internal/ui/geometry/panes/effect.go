package panes

import uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"

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
type ResizeRequest struct{ Resize }

func (ResizeRequest) EffectName() string { return "pane resize" }

func (r ResizeRequest) EffectCopy() uieffects.Request {
	r.Resize = Resize{Publish: r.Publish, Targets: append([]PaneResize(nil), r.Targets...)}
	return r
}

func (r ResizeRequest) Run(s Services) (uieffects.Result, error) {
	return Run(s.Store, s.Driver, s.Reflower, r.Resize)
}

// SplitSaveRequest persists a committed divider ratio; the job that failed
// owns the status bar through its id.
type SplitSaveRequest struct{ Value string }

func (SplitSaveRequest) EffectName() string { return "split ratio save" }

func (r SplitSaveRequest) Run(s Services) (uieffects.Result, error) {
	if s.Store == nil {
		return nil, ErrNoStore
	}
	return nil, SaveRatio(s.Store, r.Value)
}
