package status

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/config"
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
	blockedLine    *regexp.Regexp
	trailingNote   *regexp.Regexp
	busyLine       *regexp.Regexp
	limitLine      *regexp.Regexp
	messageStart   *regexp.Regexp
	placeholder    *regexp.Regexp
	userEcho       *regexp.Regexp
	dialogFooter   *regexp.Regexp
	// composerPlaceholder is the literal text a tool paints inside its
	// empty composer; a draft replaces it. Searched in a stripped row.
	composerPlaceholder string
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
		tr := toolRules{defaultStatus: def, composerPlaceholder: tool.ComposerPlaceholder, rules: compiled}
		optional := []struct {
			pattern string
			target  **regexp.Regexp
		}{
			{tool.ActivityCutoff, &tr.activityCutoff},
			{tool.InputPrefix, &tr.inputPrefix},
			{tool.TurnEnd, &tr.turnEnd},
			{tool.ChromeLine, &tr.chromeLine},
			{tool.BlockedLine, &tr.blockedLine},
			{tool.TrailingNote, &tr.trailingNote},
			{tool.BusyLine, &tr.busyLine},
			{tool.LimitLine, &tr.limitLine},
			{tool.MessageStart, &tr.messageStart},
			{tool.InputPlaceholder, &tr.placeholder},
			{tool.UserEcho, &tr.userEcho},
			{tool.DialogFooter, &tr.dialogFooter},
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
// outlives it. Background agents and background shells keep going after
// the turn that spawned them ends, and the line saying so carries the same
// shape as a turn-end summary, so turnState would otherwise read the turn
// as over while the session is still busy. Only a turn that ended below the
// busy line proves that work drained; transient banners under it say nothing
// either way. Without turn_end there is no later turn to read, so the line
// stands until the tool stops drawing it.
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

func (tr toolRules) hasWaitingFooter(cutoffTail string) bool {
	lineEnd := strings.IndexByte(cutoffTail, '\n')
	if lineEnd < 0 {
		return false
	}
	footer := cutoffTail[lineEnd+1:]
	if tr.dialogFooter != nil && tr.dialogFooter.MatchString(footer) {
		return true
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

// LastMessage is the tool's newest message, flattened to one line: the
// content lines above the input box, from the last message_start marker
// on, joined in order — so a caller quoting the reply starts at its
// beginning and fits as much of it as the row can hold. Chrome, busy
// spinners and turn_end markers are stepped over, and a tool without a
// marker yields its newest content line alone. anchored reports that a
// marker was found — false means the quote is the newest content line,
// which for a marker tool is the sign the message start scrolled out of
// the captured text. ok is false when the tool has no activity_cutoff to
// find the box with, or the cutoff is absent from the pane.
func (e *Engine) LastMessage(tool, pane string) (line string, anchored, ok bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return "", false, false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return "", false, false
	}
	lines := strings.Split(region, "\n")
	structural := func(line string) bool {
		if tr.chromeLine != nil && tr.chromeLine.MatchString(line) {
			return true
		}
		if tr.busyLine != nil && tr.busyLine.MatchString(line) {
			return true
		}
		if tr.turnEnd != nil && tr.turnEnd.MatchString(line) {
			return true
		}
		return tr.matchesWorkingRule(line)
	}
	start, lastContent := -1, -1
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" || structural(line) {
			continue
		}
		lastContent = i
		if tr.messageStart != nil && tr.messageStart.MatchString(line) {
			start = i
		}
	}
	if lastContent == -1 {
		return "", false, true
	}
	if start == -1 {
		return strings.TrimSpace(lines[lastContent]), false, true
	}
	// The message runs from its marker until the next structural line: a
	// turn summary or a rule closes it, so a notice printed after the
	// turn (a plugin banner, a warning) is not glued onto the reply.
	first := strings.TrimRight(lines[start], " \t")
	if loc := tr.messageStart.FindStringIndex(first); loc != nil {
		first = first[loc[1]:]
	}
	parts := []string{strings.TrimSpace(first)}
	for _, raw := range lines[start+1:] {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if structural(line) {
			break
		}
		parts = append(parts, strings.TrimSpace(line))
	}
	return strings.TrimSpace(strings.Join(parts, " ")), true, true
}

// HasMessageStart reports whether the tool declared a message_start
// marker, i.e. whether an unanchored LastMessage means the marker
// scrolled away rather than never existing.
func (e *Engine) HasMessageStart(tool string) bool {
	tr, ok := e.tools[tool]
	return ok && tr.messageStart != nil
}

// HasUserEcho reports whether the tool echoes submitted prompts into its
// transcript in a recognisable shape.
func (e *Engine) HasUserEcho(tool string) bool {
	tr, ok := e.tools[tool]
	return ok && tr.userEcho != nil
}

// LastUserEcho is the newest prompt the tool echoed into its transcript,
// past the echo marker: the last thing sent to the session, whoever sent
// it and from wherever it was typed. Empty means no echo is in the
// captured text; ok is false when the tool has no user_echo or no
// activity_cutoff to bound the transcript with.
func (e *Engine) LastUserEcho(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok || tr.userEcho == nil {
		return "", false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return "", false
	}
	lines := strings.Split(region, "\n")
	// A composer drawn above the cutoff (opencode's ┃ gutter) is a run of
	// input_prefix rows hugging the region's end; the echoes live higher,
	// so the trailing run is the composer's, not a message.
	if tr.inputPrefix != nil {
		for len(lines) > 0 {
			last := lines[len(lines)-1]
			if strings.TrimSpace(last) == "" || tr.inputPrefix.MatchString(last) {
				lines = lines[:len(lines)-1]
				continue
			}
			break
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], " \t")
		loc := tr.userEcho.FindStringIndex(line)
		if loc == nil {
			continue
		}
		// A dialog draws its option rows behind the same marker the
		// composer uses (codex's "› 1. Yes, continue"), so a line any
		// status rule recognises is the tool's frame, not an echo.
		if tr.matchesAnyRule(line) {
			continue
		}
		echoed := strings.TrimSpace(line[loc[1]:])
		if echoed == "" {
			continue
		}
		if tr.placeholder != nil && tr.placeholder.MatchString(echoed) {
			continue
		}
		return echoed, true
	}
	return "", true
}

func (tr toolRules) matchesAnyRule(line string) bool {
	for _, r := range tr.rules {
		if r.re.MatchString(line) {
			return true
		}
	}
	return false
}

// InputDraft is the text typed into the tool's composer: what follows the
// last activity_cutoff match on its own row. A placeholder the composer
// paints on the empty row is the tool's wording, not a draft, and a tool
// whose composer sits above its cutoff (opencode, pi) cannot be read this
// way; ok is false for all of those.
func (e *Engine) InputDraft(tool, pane string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok || tr.activityCutoff == nil {
		return "", false
	}
	// An input_prefix declares a composer drawn above the cutoff
	// (opencode's ┃ box over ╹), so the text after a cutoff match is the
	// composer's frame, never a draft.
	if tr.inputPrefix != nil {
		return "", false
	}
	locs := tr.activityCutoff.FindAllStringIndex(pane, -1)
	if len(locs) == 0 {
		return "", false
	}
	rest := pane[locs[len(locs)-1][1]:]
	if lineEnd := strings.IndexByte(rest, '\n'); lineEnd >= 0 {
		rest = rest[:lineEnd]
	}
	draft := strings.TrimSpace(rest)
	if draft == "" {
		return "", false
	}
	if tr.placeholder != nil && tr.placeholder.MatchString(draft) {
		return "", false
	}
	return draft, true
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

// InputPrefix returns the prompt marker a tool draws at the start of its
// input line, when row is that line. A tool may declare its own marker with
// input_prefix, which replaces the reuse of activity_cutoff here; one
// written on purpose may be zero-width, since a markerless composer (pi's
// blank row) still needs to be recognisable. Without that override the
// check reuses activity_cutoff, matched against a single row and anchored
// at its start, so a marker quoted further along the row cannot pass. A
// zero-width fallback match is no marker: a degenerate cutoff like ^ would
// otherwise stamp every row as a prompt.
func (e *Engine) InputPrefix(tool, row string) (string, bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return "", false
	}
	override := tr.inputPrefix != nil
	cut := tr.inputPrefix
	if cut == nil {
		cut = tr.activityCutoff
		if cut == nil {
			return "", false
		}
	}
	loc := cut.FindStringIndex(row)
	if loc == nil || loc[0] != 0 || (loc[1] == 0 && !override) {
		return "", false
	}
	return row[:loc[1]], true
}

// MatchesActivityCutoff reports whether a single row opens with the tool's
// activity cutoff, i.e. is one of the rows that bound its input box. The
// arrow-step head check skips such rows when reading context above the
// caret, so a composer bounded by a rule (pi) reads cleanly.
func (e *Engine) MatchesActivityCutoff(tool, row string) bool {
	tr, ok := e.tools[tool]
	if !ok {
		return false
	}
	return tr.inputRow(row)
}

// inputRow reports whether a row opens with the tool's activity cutoff. A
// zero-width match is no marker, the same way InputPrefix reads one: a
// degenerate cutoff like ^ would otherwise stamp every row as input.
// InputPrefix's zero-width escape hatch is for an explicitly declared
// prefix (pi's ^); a cutoff never earns it.
func (tr toolRules) inputRow(row string) bool {
	if tr.activityCutoff == nil {
		return false
	}
	loc := tr.activityCutoff.FindStringIndex(row)
	return loc != nil && loc[0] == 0 && loc[1] > 0
}

// ComposerIsEmpty reports whether this composer row holds nothing to edit,
// which is how an empty composer is told from a draft for tools whose
// terminal cursor never enters the composer. Empty is either the
// placeholder a tool paints on a pristine prompt or nothing after the
// marker at all: command-code paints its placeholder until the first prompt
// is typed and never again, so the bare marker a cleared composer leaves
// behind is just as empty. A draft merely containing or ending with the
// placeholder stays a draft. False for a tool that declares no placeholder,
// which keeps every other tool on the marker rules.
func (e *Engine) ComposerIsEmpty(tool, row string) bool {
	tr, ok := e.tools[tool]
	if !ok || tr.composerPlaceholder == "" {
		return false
	}
	prefix, ok := e.InputPrefix(tool, row)
	if !ok {
		return false
	}
	rest := strings.TrimSpace(row[len(prefix):])
	return rest == "" || rest == tr.composerPlaceholder
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
