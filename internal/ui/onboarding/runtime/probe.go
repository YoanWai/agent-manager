package runtime

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/systheme"
	"github.com/YoanWai/agent-manager/internal/ui/effects"
	"maps"
)

type Services struct{}
type ProbeRequest struct {
	Target   uint64
	Commands map[string]string
}
type ProbeResult struct {
	Installed map[string]bool
	Scheme    systheme.Scheme
}

func (ProbeRequest) EffectName() string            { return "onboarding-probe" }
func (r ProbeRequest) EffectCopy() effects.Request { r.Commands = maps.Clone(r.Commands); return r }
func (ProbeResult) EffectResult()                  {}
func (r ProbeRequest) Run(Services) (effects.Result, error) {
	installed := make(map[string]bool, len(r.Commands))
	for name, command := range r.Commands {
		installed[name] = config.CheckInstalled(command) == nil
	}
	return ProbeResult{Installed: installed, Scheme: systheme.Detect()}, nil
}
