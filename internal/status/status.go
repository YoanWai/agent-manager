package status

import (
	"regexp"
	"strings"

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
	turnEnd        *regexp.Regexp
	chromeLine     *regexp.Regexp
	blockedLine    *regexp.Regexp
	trailingNote   *regexp.Regexp
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
		optional := []struct {
			pattern string
			target  **regexp.Regexp
		}{
			{tool.ActivityCutoff, &tr.activityCutoff},
			{tool.TurnEnd, &tr.turnEnd},
			{tool.ChromeLine, &tr.chromeLine},
			{tool.BlockedLine, &tr.blockedLine},
			{tool.TrailingNote, &tr.trailingNote},
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
// caller can distinguish a real signal from the default fallback. Rules
// run first, scoped to the current turn; when none hit, the newest turn
// in the content region decides finished versus waiting.
func (e *Engine) Match(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return Idle, false
	}
	scope := tr.matchScope(pane)
	for _, r := range tr.rules {
		if r.re.MatchString(scope) {
			return r.state, true
		}
	}
	if state, ok := tr.turnState(pane); ok {
		return state, true
	}
	return tr.defaultStatus, false
}

// matchScope narrows rule matching to the current turn: the text after
// the newest turn_end marker in the content region. Completed turns can
// quote spinner lines or dialog text verbatim (any session working on
// terminal tooling will), and whole-pane matching would read those
// echoes as live signals. Panes without a marker (fresh sessions,
// dialogs that replace the input box) match in full.
func (tr toolRules) matchScope(pane string) string {
	if tr.turnEnd == nil {
		return pane
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return pane
	}
	lines := strings.Split(region, "\n")
	if lastEnd := tr.lastTurnEndIndex(lines); lastEnd >= 0 {
		return strings.Join(lines[lastEnd+1:], "\n")
	}
	return pane
}

// lastTurnEndIndex finds the newest turn_end marker line, -1 when absent.
func (tr toolRules) lastTurnEndIndex(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if tr.turnEnd.MatchString(strings.TrimRight(lines[i], " \t")) {
			return i
		}
	}
	return -1
}

// ActivityRegion returns the pane content above the tool's input box
// (the last activity_cutoff match). Streaming output changes this region
// between polls even when no status rule matches. ok is false when the
// tool has no cutoff configured or it does not appear in the pane.
func (e *Engine) ActivityRegion(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return "", false
	}
	return tr.activityRegion(pane)
}

func (tr toolRules) activityRegion(pane string) (string, bool) {
	if tr.activityCutoff == nil {
		return "", false
	}
	locs := tr.activityCutoff.FindAllStringIndex(pane, -1)
	if len(locs) == 0 {
		return "", false
	}
	return pane[:locs[len(locs)-1][0]], true
}

// turnState inspects the newest turn in the content region. When nothing
// but chrome (blanks, separators) and trailing notes (recap blocks) sits
// below the last turn_end marker, the turn just ended: finished, or
// waiting when the content line above the marker carries a question mark
// (the agent asked something in plain text). A blocked_line as the last
// content (e.g. an interrupt banner) also waits on the user. Anchoring on
// the newest marker means markers from older turns, still visible higher
// in the pane, can never retrigger.
func (tr toolRules) turnState(pane string) (string, bool) {
	if tr.turnEnd == nil {
		return "", false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return "", false
	}
	lines := strings.Split(region, "\n")
	last := lastContentIndex(lines, len(lines)-1, tr.chromeLine)
	if last < 0 {
		return "", false
	}
	if tr.blockedLine != nil && tr.blockedLine.MatchString(lines[last]) {
		return Waiting, true
	}

	lastEnd := tr.lastTurnEndIndex(lines)
	if lastEnd < 0 || !tr.turnIsNewest(lines[lastEnd+1:]) {
		return "", false
	}
	question := lastContentIndex(lines, lastEnd-1, nil)
	if question >= 0 && strings.Contains(lines[question], "?") {
		return Waiting, true
	}
	return Finished, true
}

// TurnEndedState infers the resting status of a turn that closed without
// a turn_end marker: the poller calls it when a region that was working
// stops changing and no rule matches. A question mark on the last content
// line means the agent asked something in plain text and waits on the
// answer; anything else counts as finished.
func (e *Engine) TurnEndedState(tool, region string) string {
	tr, ok := e.tools[tool]
	if !ok {
		return Finished
	}
	lines := strings.Split(region, "\n")
	last := lastContentIndex(lines, len(lines)-1, tr.chromeLine)
	if last >= 0 && strings.Contains(lines[last], "?") {
		return Waiting
	}
	return Finished
}

// turnIsNewest reports whether the lines below a turn_end marker hold no
// real content: only blanks, chrome, and trailing note blocks. Any other
// content means a newer turn is already producing output.
func (tr toolRules) turnIsNewest(after []string) bool {
	inNote := false
	for _, line := range after {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		if tr.chromeLine != nil && tr.chromeLine.MatchString(trimmed) {
			continue
		}
		if tr.trailingNote != nil && tr.trailingNote.MatchString(strings.TrimLeft(trimmed, " \t")) {
			inNote = true
			continue
		}
		if inNote {
			continue
		}
		return false
	}
	return true
}

// lastContentIndex walks upward from start to the nearest line that is
// neither blank nor chrome (separators, input-box borders).
func lastContentIndex(lines []string, start int, chrome *regexp.Regexp) int {
	for i := start; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if chrome != nil && chrome.MatchString(strings.TrimRight(lines[i], " \t")) {
			continue
		}
		return i
	}
	return -1
}
