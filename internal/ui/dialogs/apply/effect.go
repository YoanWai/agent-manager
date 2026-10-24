package apply

import uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"

// ForkEffect freezes a fork dialog submission; ForkRequest carries it and Run
// does the work.
type ForkEffect struct{ ForkRequest }

func (ForkEffect) EffectName() string { return "fork" }

func (e ForkEffect) EffectCopy() uieffects.Request {
	e.ForkRequest = e.Copy()
	return e
}

func (e ForkEffect) Run(services Services) (uieffects.Result, error) {
	result, err := Fork(services.Sources, services.Preflight, e.ForkRequest)
	return ForkEffectResult{Outcome: result}, err
}

// ForkEffectResult is the durable outcome of a fork.
type ForkEffectResult struct{ ForkOutcome }

func (ForkEffectResult) EffectResult() {}

// RenameEffect carries what the rename worker needs; Request holds the values
// and Run does the work.
type RenameEffect struct{ Request }

func (RenameEffect) EffectName() string { return "rename" }

func (e RenameEffect) EffectCopy() uieffects.Request {
	e.Request = e.Copy()
	return e
}

func (e RenameEffect) Run(services Services) (uieffects.Result, error) {
	result, err := Rename(services.Sources, services.Preflight, e.Request)
	return RenameEffectResult{Outcome: result}, err
}

// RenameEffectResult reports which durable stages committed, in order.
type RenameEffectResult struct{ Outcome }

func (RenameEffectResult) EffectResult() {}

// MoveClose is the chained follow-up that closes a move dialog only if the
// dialog it captured is still the open one.
type MoveClose struct {
	IsGroup   bool
	Gen       uint64
	SessID    string
	Group     string
	Parent    string
	OptPath   string
	OptSessID string
}

func (MoveClose) EffectName() string { return "move" }

func (c MoveClose) EffectCopy() uieffects.Request { return c }

func (MoveClose) Run(Services) (uieffects.Result, error) { return MoveCloseResult{}, nil }

// MoveCloseResult is the answer to a move close that has nothing to report.
type MoveCloseResult struct{}

func (MoveCloseResult) EffectResult() {}

// Services is what a dialog worker reaches off the update path: the store and
// git writes its sources hold, and the read-only path probe it resolves
// against.
type Services struct {
	Sources   Sources
	Preflight Preflight
}
