package status

import (
	"strings"
)

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
	return tr.settledBelow(after, false)
}

// limitIsNewest is turnIsNewest plus the first turn-end summary below the
// banner, which closed the limited turn. A second summary is a newer turn.
func (tr toolRules) limitIsNewest(after []string) bool {
	return tr.settledBelow(after, true)
}

func (tr toolRules) settledBelow(after []string, skipTurnEnd bool) bool {
	inNote := false
	for _, line := range after {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		if tr.chromeLine != nil && tr.chromeLine.MatchString(trimmed) {
			continue
		}
		if skipTurnEnd && tr.turnEnd != nil && tr.turnEnd.MatchString(trimmed) {
			skipTurnEnd = false
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
