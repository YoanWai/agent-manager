package status

import (
	"regexp"

	"github.com/YoanWai/agent-manager/internal/config"
)

const (
	Working  = "working"
	Waiting  = "waiting"
	Finished = "finished"
	Errored  = "errored"
	Idle     = "idle"
	Dead     = "dead"
)

type rule struct {
	state string
	re    *regexp.Regexp
}

type Engine struct {
	tools map[string]toolRules
}

type toolRules struct {
	defaultStatus  string
	activityCutoff *regexp.Regexp
	rules          []rule
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
		tr := toolRules{defaultStatus: def, rules: compiled}
		if tool.ActivityCutoff != "" {
			re, err := regexp.Compile(tool.ActivityCutoff)
			if err != nil {
				return nil, err
			}
			tr.activityCutoff = re
		}
		engine.tools[name] = tr
	}
	return engine, nil
}

func (e *Engine) Derive(tool, pane string) string {
	state, _ := e.Match(tool, pane)
	return state
}

// Match derives a status and reports whether any rule matched, so the
// caller can distinguish a real signal from the default fallback.
func (e *Engine) Match(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return Idle, false
	}
	for _, r := range tr.rules {
		if r.re.MatchString(pane) {
			return r.state, true
		}
	}
	return tr.defaultStatus, false
}

// ActivityRegion returns the pane content above the tool's input box
// (the last activity_cutoff match). Streaming output changes this region
// between polls even when no status rule matches. ok is false when the
// tool has no cutoff configured or it does not appear in the pane.
func (e *Engine) ActivityRegion(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok || tr.activityCutoff == nil {
		return "", false
	}
	locs := tr.activityCutoff.FindAllStringIndex(pane, -1)
	if len(locs) == 0 {
		return "", false
	}
	return pane[:locs[len(locs)-1][0]], true
}
