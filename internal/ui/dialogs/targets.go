package dialogs

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

type Agent struct {
	ID    string
	Group string
	Name  string
}

// PruneTargets drops the moved group and its descendants from the picker: a
// group cannot land inside its own subtree.
func PruneTargets(p *spawn.Picker, subtree string) {
	selected := p.GroupPath()
	options := make([]spawn.GroupOption, 0, len(p.Groups))
	for _, opt := range p.Groups {
		if opt.Path == subtree || strings.HasPrefix(opt.Path, subtree+"/") {
			continue
		}
		options = append(options, opt)
	}
	p.Groups = options
	p.Reselect(selected, func(spawn.GroupOption) bool { return true })
}

// NestAgents adds each agent under the group it runs in, so a shell moved
// out of an agent follows it there.
func NestAgents(p *spawn.Picker, agents []Agent) {
	selected := p.GroupPath()
	var options []spawn.GroupOption
	for _, opt := range p.Groups {
		options = append(options, opt)
		for _, agent := range agents {
			if agent.Group != opt.Path {
				continue
			}
			options = append(options, spawn.GroupOption{
				Path:      agent.Group,
				Depth:     opt.Depth + 1,
				SessionID: agent.ID,
				Name:      agent.Name,
			})
		}
	}
	p.Groups = options
	p.Reselect(selected, func(opt spawn.GroupOption) bool { return opt.SessionID == "" })
}
