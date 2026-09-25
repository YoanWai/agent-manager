package status

import (
	"strings"
	"unicode"
)

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

// TypingHold reports why text typed into this pane now would land somewhere
// it is not read as a message: Working while the tool is mid-turn or has not
// drawn its input line, and Waiting while its own rules see a dialog, which
// typed text would answer rather than be read by. An empty string means the
// pane rests at a prompt that reads what it is handed, which includes a
// question the agent left on screen: that trips no rule. Only those two rule
// states hold, since a tool whose rules also classify resting frames (pi
// marks a resumed session idle) would otherwise never take anything again.
func (e *Engine) TypingHold(tool, pane string) string {
	if _, ready := e.ActivityRegion(tool, pane); !ready {
		return Working
	}
	if state, matched := e.RuleMatch(tool, pane); matched && (state == Working || state == Waiting) {
		return state
	}
	return ""
}

// isBusy reports whether the newest turn is still running work that
// outlives it. Background agents keep going after the turn that spawned
// them ends, and the line saying so carries the same shape as a turn-end
// summary, so turnState would otherwise read the turn as over while the
// session is still busy. Only a turn that ended below the busy line proves
// that work drained; transient banners under it say nothing either way.
// Without turn_end there is no later turn to read, so the line stands until
// the tool stops drawing it.
func (tr toolRules) isBusy(pane string) bool {
	if tr.busyLine == nil {
		return false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return false
	}
	lines := strings.Split(region, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !tr.busyLine.MatchString(strings.TrimRight(lines[i], " \t")) {
			continue
		}
		return tr.turnEnd == nil || tr.lastTurnEndIndex(lines) <= i
	}
	return false
}

// matchScope narrows rule matching to the current turn: the text after
// the newest turn_end marker in the content region. Completed turns can
// quote spinner lines or dialog text verbatim (any session working on
// terminal tooling will), and whole-pane matching would read those
// echoes as live signals. Dialogs that replace the input box match in full.
// With an input box but no marker, matching stays in the content region so
// typed input cannot masquerade as a status signal.
func (tr toolRules) matchScope(pane string) string {
	if tr.turnEnd == nil {
		return pane
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return pane
	}
	cutoffTail := pane[len(region):]
	hasWaitingFooter := tr.hasWaitingFooter(cutoffTail)
	lines := strings.Split(region, "\n")
	if lastEnd := tr.lastTurnEndIndex(lines); lastEnd >= 0 {
		scope := tr.withoutInputRows(lines[lastEnd+1:])
		if hasWaitingFooter {
			return scope + cutoffTail
		}
		return scope
	}
	// Some selection dialogs reuse the prompt marker as their first option.
	// Keep treating ordinary typed input as outside the match scope, but include
	// the full pane when a separate waiting signal appears below that marker.
	// Codex overlays render such a footer, and claude's question dialog names
	// it in dialog_footer; the selected option line alone is indistinguishable
	// from a numbered draft and must not expand the scope.
	if hasWaitingFooter {
		return pane
	}
	return tr.withoutInputRows(lines)
}

// withoutInputRows joins region rows, dropping the messages the user
// already sent. The tool replays them above its composer wearing the same
// marker, so a numbered list they typed is otherwise indistinguishable
// from a dialog's selected option, and text they quoted from another pane
// reads as that pane's live signal. A replayed message runs from its
// marker row until a row opens a block of its own.
func (tr toolRules) withoutInputRows(lines []string) string {
	kept := make([]string, 0, len(lines))
	sent := false
	for _, line := range lines {
		if tr.inputRow(line) {
			sent = true
			continue
		}
		if sent && wrapsAbove(line) {
			continue
		}
		sent = false
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// wrapsAbove reports whether a row belongs to the block above it rather
// than starting one: tools indent what wraps and leave the blank rows
// between blocks empty.
func wrapsAbove(row string) bool {
	body := strings.TrimLeftFunc(row, unicode.IsSpace)
	return body == "" || len(body) < len(row)
}

// turnDied reports a working signal the tool no longer backs: it paints a
// busy_footer for as long as a turn runs, and the footer has gone back to
// its resting form while the working marker is still on screen.
func (tr toolRules) turnDied(pane string) bool {
	if tr.busyFooter == nil {
		return false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return false
	}
	footer, ok := footerBelow(pane[len(region):])
	return ok && !tr.busyFooter.MatchString(footer)
}

func footerBelow(cutoffTail string) (string, bool) {
	lineEnd := strings.IndexByte(cutoffTail, '\n')
	if lineEnd < 0 {
		return "", false
	}
	return cutoffTail[lineEnd+1:], true
}

func (tr toolRules) hasWaitingFooter(cutoffTail string) bool {
	if tr.dialogOpen(cutoffTail) {
		return true
	}
	footer, ok := footerBelow(cutoffTail)
	if !ok {
		return false
	}
	for _, r := range tr.rules {
		if r.state == Waiting && r.re.MatchString(footer) {
			return true
		}
	}
	return false
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

func (tr toolRules) matchesAnyRule(line string) bool {
	for _, r := range tr.rules {
		if r.re.MatchString(line) {
			return true
		}
	}
	return false
}

// matchesWorkingRule reports whether a line is one of the tool's working
// signals — a spinner row, an interrupt hint — which narrate the turn
// rather than say anything, so a caller quoting output steps over them.
func (tr toolRules) matchesWorkingRule(line string) bool {
	for _, r := range tr.rules {
		if r.state == Working && r.re.MatchString(line) {
			return true
		}
	}
	return false
}
