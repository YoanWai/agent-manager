package lifecycle

import uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"

func (AttachRequest) EffectName() string { return "focus or attach" }

func (r AttachRequest) EffectCopy() uieffects.Request { return r }

func (r AttachRequest) Run(services Services) (uieffects.Result, error) {
	return RunAttach(services, r)
}

func (AttachResult) EffectResult() {}
