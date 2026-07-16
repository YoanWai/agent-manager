package status

import (
	"regexp"

	"github.com/YoanWai/agent-manager/internal/config"
)

const (
	Working = "working"
	Waiting = "waiting"
	Ready   = "ready"
	Errored = "errored"
	Idle    = "idle"
	Dead    = "dead"
)

type rule struct {
	state string
	re    *regexp.Regexp
}

type Engine struct {
	tools map[string]toolRules
}

type toolRules struct {
	defaultStatus string
	rules         []rule
}

func NewEngine(cfg config.Config) (*Engine, error) {
	engine := &Engine{tools: map[string]toolRules{}}
	for name, tool := range cfg.Tools {
		compiled := make([]rule, 0, len(tool.Rules))
		for _, raw := range tool.Rules {
			re, err := regexp.Compile(raw.Pattern)
			if err != nil {
				return nil, err
			}
			compiled = append(compiled, rule{state: raw.State, re: re})
		}
		def := tool.DefaultStatus
		if def == "" {
			def = Idle
		}
		engine.tools[name] = toolRules{defaultStatus: def, rules: compiled}
	}
	return engine, nil
}

func (e *Engine) Derive(tool, pane string) string {
	tr, ok := e.tools[tool]
	if !ok {
		return Idle
	}
	for _, r := range tr.rules {
		if r.re.MatchString(pane) {
			return r.state
		}
	}
	return tr.defaultStatus
}
