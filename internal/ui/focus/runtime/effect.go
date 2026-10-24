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

func (s Services) writer() Writer {
	return Writer{Store: s.Store, Driver: s.Driver, Engine: s.Engine, Watch: s.Watch, Paste: s.Paste}
}

// ProbeRequest enters focus: the dead-pane probe runs on the effect lane, and
// the completion enters only while the selection and the foreground it was
// asked for still hold.
type ProbeRequest struct {
	Generation uint64
	SessionID  string
}

func (ProbeRequest) EffectName() string { return "focus or attach" }

func (request ProbeRequest) EffectCopy() uieffects.Request { return request }

func (request ProbeRequest) Run(services Services) (uieffects.Result, error) {
	return ProbeResult{}, services.prober().Probe(request.SessionID)
}

// ProbeResult carries no facts: the session it probed is the request's.
type ProbeResult struct{}

func (ProbeResult) EffectResult() {}

// AckRequest clears the unread mark a finished session left.
type AckRequest struct {
	SessionID string
}

func (AckRequest) EffectName() string { return "focus or attach" }

func (request AckRequest) EffectCopy() uieffects.Request { return request }

func (request AckRequest) Run(services Services) (uieffects.Result, error) {
	return AckResult{}, services.prober().Ack(request.SessionID)
}

// AckResult carries no facts: the cleared row comes back in the next poll.
type AckResult struct{}

func (AckResult) EffectResult() {}

// DetachRequest reads and clears the request a session wrote before it
// detached.
type DetachRequest struct {
	SessionID string
}

func (DetachRequest) EffectName() string { return "focus or attach" }

func (request DetachRequest) EffectCopy() uieffects.Request { return request }

func (request DetachRequest) Run(services Services) (uieffects.Result, error) {
	detached, err := services.prober().Detached(request.SessionID)
	return DetachResult{Request: detached.Request, ClearErr: detached.ClearErr}, err
}

// DetachResult is what the session left behind: the marker it wrote, and a
// failed clear, which leaves the marker for the next read.
type DetachResult struct {
	Request  string
	ClearErr error
}

func (DetachResult) EffectResult() {}

// InputRequest is one queued write to a focused session's pane.
type InputRequest struct {
	Value Request
}

func (InputRequest) EffectName() string { return "input to a session" }

func (value InputRequest) EffectCopy() uieffects.Request {
	value.Value = value.Value.Copied()
	return value
}

func (request InputRequest) Run(services Services) (uieffects.Result, error) {
	result, err := services.writer().Run(request.Value)
	return InputResult{
		InputID:   result.InputID,
		Candidate: result.Candidate,
		PromptID:  result.PromptID,
		Prompt:    result.Prompt,
	}, err
}

// InputResult reports what a write left for the Update path: the session it
// reached, the composer draft an enter captured, and the prompt a receipt
// recorded.
type InputResult struct {
	InputID   string
	Candidate *Candidate
	PromptID  string
	Prompt    string
}

func (InputResult) EffectResult() {}
