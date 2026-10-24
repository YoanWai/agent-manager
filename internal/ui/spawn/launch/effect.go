package launch

import (
	"slices"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// Request is the effect a form, the quick bar or the terminal key queues: it
// names itself, owns the slices it carries, and runs against Services.
func (Request) EffectName() string { return "spawn" }

func (r Request) EffectCopy() uieffects.Request {
	r.DirFallbacks = slices.Clone(r.DirFallbacks)
	r.TerminalDir.Fallbacks = slices.Clone(r.TerminalDir.Fallbacks)
	if r.Plan != nil {
		plan := *r.Plan
		plan.PendingInputs = slices.Clone(plan.PendingInputs)
		r.Plan = &plan
	}
	r.Images = slices.Clone(r.Images)
	return r
}

func (r Request) Run(services Services) (uieffects.Result, error) {
	return Run(services, r)
}

func (Result) EffectResult() {}

func (GroupRequest) EffectName() string { return "group creation" }

func (r GroupRequest) EffectCopy() uieffects.Request {
	r.Fallbacks = slices.Clone(r.Fallbacks)
	return r
}

func (r GroupRequest) Run(services Services) (uieffects.Result, error) {
	return RunGroup(services, r)
}

func (GroupResult) EffectResult() {}

// ChoiceSaveRequest persists a CLI's kept choice or recent models. The
// settings cache already holds the value, so the next form reads it without
// waiting for the write.
type ChoiceSaveRequest struct {
	Key   string
	Value string
}

func (ChoiceSaveRequest) EffectName() string { return "model choice save" }

func (r ChoiceSaveRequest) EffectCopy() uieffects.Request { return r }

func (r ChoiceSaveRequest) Run(services Services) (uieffects.Result, error) {
	return ChoiceSaveResult{}, SaveChoice(services.Store, r.Key, r.Value)
}

// ChoiceSaveResult carries nothing back: only its error reaches the model.
type ChoiceSaveResult struct{}

func (ChoiceSaveResult) EffectResult() {}
