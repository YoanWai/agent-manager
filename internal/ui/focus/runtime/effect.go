package runtime

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	"github.com/YoanWai/agent-manager/internal/ui/focus/watch"
)

// Services is what this package's effects run against: the store, the tmux
// server, the watcher a write travels through, the status engine and the
// paste transport.
type Services struct {
	Store  *store.Store
	Driver *tmux.Driver
	Engine *status.Engine
	Watch  *watch.Watcher
	// Paste carries pasted text into the pane.
	Paste func(*tmux.Driver, string, string) error
}

func (s Services) prober() Prober { return Prober{Store: s.Store, Driver: s.Driver} }

// ProbeRequest enters focus: the dead-pane probe runs on the effect lane, and
// the completion enters only while the selection and the foreground it was
// asked for still hold.
type ProbeRequest struct {
	Generation uint64
	SessionID  string
}

func (ProbeRequest) EffectName() string { return "focus or attach" }

func (r ProbeRequest) Run(s Services) (uieffects.Result, error) {
	return nil, s.prober().Probe(r.SessionID)
}

// AckRequest clears the unread mark a finished session left.
type AckRequest struct{ SessionID string }

func (AckRequest) EffectName() string { return "focus or attach" }

func (r AckRequest) Run(s Services) (uieffects.Result, error) {
	return nil, s.prober().Ack(r.SessionID)
}

// DetachRequest reads and clears the request a session wrote before it
// detached.
type DetachRequest struct{ SessionID string }

func (DetachRequest) EffectName() string { return "focus or attach" }

func (r DetachRequest) Run(s Services) (uieffects.Result, error) {
	return s.prober().Detached(r.SessionID)
}

// InputRequest is one queued write to a focused session's pane.
type InputRequest struct{ Value Request }

func (InputRequest) EffectName() string { return "input to a session" }

func (r InputRequest) EffectCopy() uieffects.Request {
	r.Value = r.Value.Copied()
	return r
}

func (r InputRequest) Run(s Services) (uieffects.Result, error) {
	return Writer(s).Run(r.Value)
}
