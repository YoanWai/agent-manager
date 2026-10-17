package status

import (
	"regexp"
	"strings"
	"unicode"
)

// TypingHold reports why text typed into this pane now would land somewhere
// it is not read as a message: Working while the tool is mid-turn or has not
// drawn its input line, and Waiting while its own rules see a dialog, which
// typed text would answer rather than be read by. An empty string means the
// pane rests at a prompt that reads what it is handed, which includes a
// question the agent left on screen: that trips no rule. Only those two rule
// states hold, since a tool whose rules also classify resting frames (pi
// marks a resumed session idle) would otherwise never take anything again.
func (e *Engine) TypingHold(tool, pane string) string {
	state, matched := e.RuleMatch(tool, pane)
	// A dialog may replace the input line entirely. Its specific waiting
	// signal is more useful than the generic missing-input working hold.
	if matched && state == Waiting {
		return Waiting
	}
	if _, ready := e.ActivityRegion(tool, pane); !ready {
		return Working
	}
	if !matched || (state != Working && state != Waiting) {
		return ""
	}
	// A turn that died leaves its working marker behind; Match reads that
	// pane as errored, and the resting prompt below it takes text again.
	if state == Working && e.tools[tool].turnDied(pane) {
		return ""
	}
	return state
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
		if !tr.busyLine.MatchString(unwrapped(lines, i)) {
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
// unwrapped joins row i with the indented rows a narrow pane wraps it onto,
// since a busy line's telling words sit at its end.
func unwrapped(lines []string, i int) string {
	row := strings.TrimRight(lines[i], " \t")
	for _, next := range lines[i+1:] {
		body := strings.TrimSpace(next)
		if body == "" || !wrapsAbove(next) {
			break
		}
		row += " " + body
	}
	return row
}

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
