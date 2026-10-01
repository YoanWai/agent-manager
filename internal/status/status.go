package status

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"regexp"
	"strings"
)

const (
	Working  = "working"
	Waiting  = "waiting"
	Finished = "finished"
	Errored  = "errored"
	Idle     = "idle"
	Dead     = "dead"
	// Starting is the transient state a session shows from launch until its
	// agent first draws to the pane, so a new row appears immediately instead
	// of after the next poll.
	Starting = "starting"
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
	inputPrefix    *regexp.Regexp
	turnEnd        *regexp.Regexp
	chromeLine     *regexp.Regexp
	chromeBlock    *regexp.Regexp
	blockedLine    *regexp.Regexp
	trailingNote   *regexp.Regexp
	busyLine       *regexp.Regexp
	limitLine      *regexp.Regexp
	messageStart   *regexp.Regexp
	toolResult     *regexp.Regexp
	placeholder    *regexp.Regexp
	userEcho       *regexp.Regexp
	dialogFooter   *regexp.Regexp
	busyFooter     *regexp.Regexp
	// composerPlaceholder is the literal text a tool paints inside its
	// empty composer; a draft replaces it. Searched in a stripped row.
	composerPlaceholder string
	blinkingMarker      string
	rules               []rule
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
		tr := toolRules{defaultStatus: def, composerPlaceholder: tool.ComposerPlaceholder, blinkingMarker: tool.BlinkingMarker, rules: compiled}
		optional := []struct {
			pattern string
			target  **regexp.Regexp
		}{
			{tool.ActivityCutoff, &tr.activityCutoff},
			{tool.InputPrefix, &tr.inputPrefix},
			{tool.TurnEnd, &tr.turnEnd},
			{tool.ChromeLine, &tr.chromeLine},
			{tool.ChromeBlock, &tr.chromeBlock},
			{tool.BlockedLine, &tr.blockedLine},
			{tool.TrailingNote, &tr.trailingNote},
			{tool.BusyLine, &tr.busyLine},
			{tool.LimitLine, &tr.limitLine},
			{tool.MessageStart, &tr.messageStart},
			{tool.ToolResult, &tr.toolResult},
			{tool.InputPlaceholder, &tr.placeholder},
			{tool.UserEcho, &tr.userEcho},
			{tool.DialogFooter, &tr.dialogFooter},
			{tool.BusyFooter, &tr.busyFooter},
		}
		for _, opt := range optional {
			if opt.pattern == "" {
				continue
			}
			re, err := regexp.Compile(opt.pattern)
			if err != nil {
				return nil, err
			}
			*opt.target = re
		}
		engine.tools[name] = tr
	}
	return engine, nil
}

// Match derives a status and reports whether any signal matched, so the
// caller can distinguish a real signal from the default fallback. A usage
// or rate-limit banner is errored even when a turn-end summary or a limit
// dialog would otherwise settle the turn. Rules then run scoped to the
// current turn. If the first matching rule is working, a matching waiting
// rule later in the list overrides it so persisted rule order cannot mask
// a user prompt. Every other first match returns as configured. When no
// rule hits, the newest turn in the content region decides finished
// versus waiting.
func (e *Engine) Match(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return Idle, false
	}
	if tr.isLimit(pane) {
		return Errored, true
	}
	if state, ok := tr.matchRules(tr.matchScope(pane)); ok {
		if state == Working && tr.turnDied(pane) {
			return Errored, true
		}
		return state, true
	}
	if tr.isBusy(pane) {
		return Working, true
	}
	if state, ok := tr.turnState(pane); ok {
		return state, true
	}
	return tr.defaultStatus, false
}

func (tr toolRules) matchRules(scope string) (string, bool) {
	for i, r := range tr.rules {
		if !r.re.MatchString(scope) {
			continue
		}
		if r.state == Working {
			for _, later := range tr.rules[i+1:] {
				if later.state == Waiting && later.re.MatchString(scope) {
					return Waiting, true
				}
			}
		}
		return r.state, true
	}
	return "", false
}

// RuleMatch reports what the tool's configured rules see, without the
// limit, busy and turn-end fallbacks Match layers on top. A modal dialog always
// trips a rule, while a question left on screen at a resting prompt does
// not, which is how a caller tells "do not type here" from "waiting for
// an answer".
func (e *Engine) RuleMatch(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return "", false
	}
	return tr.matchRules(tr.matchScope(pane))
}

// isLimit reports whether the newest turn is sitting on a usage or rate
// limit. The banner lives above the turn-end summary, so matchScope never
// sees it, and turnState would settle the quiet turn as finished. A limit
// dialog can also look like a waiting prompt.
func (tr toolRules) isLimit(pane string) bool {
	if tr.limitLine == nil {
		return false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return tr.limitLine.MatchString(pane)
	}
	lines := strings.Split(region, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !tr.limitLine.MatchString(strings.TrimRight(lines[i], " \t")) {
			continue
		}
		end := i + 1
		for end < len(lines) {
			line := lines[end]
			if strings.TrimSpace(line) == "" || (line[0] != ' ' && line[0] != '\t') {
				break
			}
			end++
		}
		return tr.limitIsNewest(lines[end:])
	}
	return false
}
