package lifecycle

import (
	"maps"
	"slices"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// EffectName is the action as a quit notice names it: the confirm card
// spells a delete with its zero value, so the package names it here.
func (r Request) EffectName() string {
	if r.Target.Action == Remove {
		return "session delete"
	}
	return "session " + string(r.Target.Action)
}

// EffectCopy owns the rows a run re-reads, so an edit of the loaded list made
// after the confirmation cannot retarget queued work.
func (r Request) EffectCopy() uieffects.Request {
	r.Target.Sessions = slices.Clone(r.Target.Sessions)
	for i := range r.Target.Sessions {
		r.Target.Sessions[i].PendingInputs = slices.Clone(r.Target.Sessions[i].PendingInputs)
		r.Target.Sessions[i].RelaunchSnapshot = maps.Clone(r.Target.Sessions[i].RelaunchSnapshot)
	}
	return r
}

func (r Request) Run(services Services) (uieffects.Result, error) {
	return Run(services, r)
}

func (Result) EffectResult() {}

func (AfterTurnRequest) EffectName() string { return "after-turn archive or kill" }

func (r AfterTurnRequest) EffectCopy() uieffects.Request { return r }

func (r AfterTurnRequest) Run(services Services) (uieffects.Result, error) {
	return RunAfterTurn(services, r)
}

func (AfterTurnResult) EffectResult() {}
