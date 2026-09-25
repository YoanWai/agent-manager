package status

import (
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

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
// marker yields its newest content line alone. An open question dialog
// draws its question in place of a message, so the question is the quote.
// anchored reports that the quote opens where its message does, on a
// marker or a dialog's question — false means the quote is the newest
// content line, which for a marker tool is the sign the message start
// scrolled out of the captured text. ok is false when the tool has no
// activity_cutoff to find the box with, or the cutoff is absent from the
// pane.
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
	inBlock := tr.chromeBlockRows(lines)
	if tr.dialogOpen(pane[len(region):]) {
		if question := tr.dialogQuestion(lines, inBlock); question != "" {
			return question, true, true
		}
	}
	start, lastContent := -1, -1
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" || inBlock[i] || tr.isStructural(line) {
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
	marker := tr.messageStart.FindStringIndex(first)
	first = first[marker[1]:]
	parts := []string{strings.TrimSpace(first)}
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if inBlock[i] || tr.isStructural(line) {
			break
		}
		parts = append(parts, strings.TrimSpace(line))
	}
	return strings.TrimSpace(strings.Join(parts, " ")), true, true
}

func (tr toolRules) dialogOpen(cutoffTail string) bool {
	footer, ok := footerBelow(cutoffTail)
	return ok && tr.dialogFooter != nil && tr.dialogFooter.MatchString(footer)
}

// dialogQuestion is the newest left-edge row of the dialog. Rows under the
// question, once the selection moves down, are the options above it, their
// descriptions and the rule some options sit under; a message above the
// dialog means it asks nothing at the left edge.
func (tr toolRules) dialogQuestion(lines []string, inBlock []bool) string {
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(line) == "" || inBlock[i] || wrapsAbove(line) || tr.isStructural(line) {
			continue
		}
		if tr.messageStart != nil && tr.messageStart.MatchString(line) {
			return ""
		}
		return line
	}
	return ""
}

// blankedMarker is a row opening on one styled cell captured as a space,
// the shape a blinking marker's off frame takes under capture-pane -e.
var blankedMarker = regexp.MustCompile(`^((?:\x1b\[[0-9;:]*m)+) (\x1b\[39m )`)

// Plain is a captured pane without its escape sequences. A tool whose
// message marker blinks gets the marker written back into the cell its off
// frame left blank, so a running step reads the same in both frames.
func (e *Engine) Plain(tool, pane string) string {
	tr, ok := e.tools[tool]
	if !ok || tr.blinkingMarker == "" {
		return ansi.Strip(pane)
	}
	lines := strings.Split(pane, "\n")
	for i, line := range lines {
		lines[i] = blankedMarker.ReplaceAllString(line, "${1}"+tr.blinkingMarker+"${2}")
	}
	return ansi.Strip(strings.Join(lines, "\n"))
}

// chromeBlockRows marks the rows of each chrome_block: the matching row and
// every row drawn straight under it, up to the next blank row.
func (tr toolRules) chromeBlockRows(lines []string) []bool {
	inBlock := make([]bool, len(lines))
	if tr.chromeBlock == nil {
		return inBlock
	}
	open := false
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			open = false
			continue
		}
		open = open || tr.chromeBlock.MatchString(line)
		inBlock[i] = open
	}
	return inBlock
}

// isStructural reports whether line is the tool's own frame - chrome, a
// spinner, a turn summary, a trailing note - rather than message content.
// LastMessage and FullTurnText share it so a rule added to one is never
// missed by the other.
func (tr toolRules) isStructural(line string) bool {
	if tr.chromeLine != nil && tr.chromeLine.MatchString(line) {
		return true
	}
	if tr.busyLine != nil && tr.busyLine.MatchString(line) {
		return true
	}
	if tr.turnEnd != nil && tr.turnEnd.MatchString(line) {
		return true
	}
	if tr.trailingNote != nil && tr.trailingNote.MatchString(strings.TrimLeft(line, " \t")) {
		return true
	}
	return tr.matchesWorkingRule(line)
}

// FullTurnText is the newest turn's prose with its paragraph breaks kept:
// everything the agent wrote after the last prompt, without tool results
// or the tool's own frame. LastMessage anchors to one message_start
// marker, which drops every earlier paragraph of a reply that opened
// several.
//
// bounded says a prompt echo or a turn summary marked where the turn
// began. Where neither is in frame the text is the whole region, which
// can hold several turns: grok keeps no prompt in its transcript, and
// any tool's summary can sit above the capture.
//
// ok is false where no reply can be read at all: the tool declares no
// activity_cutoff, the pane holds none, or its region is frame only, as
// pi's is by design.
func (e *Engine) FullTurnText(tool, pane string) (text string, bounded, ok bool) {
	tr, ok := e.tools[tool]
	if !ok {
		return "", false, false
	}
	region, ok := tr.activityRegion(pane)
	if !ok {
		return "", false, false
	}
	lines := strings.Split(region, "\n")
	fromPane := false
	if !slices.ContainsFunc(lines, tr.isContent) {
		// pi opens its region at the pane origin on purpose, so that a
		// reflow can never read as fresh output. Nothing is there to copy,
		// and the pane itself is what the user is looking at.
		lines, fromPane = tr.paneAboveComposer(pane), true
		if !slices.ContainsFunc(lines, tr.isContent) {
			return "", false, false
		}
	}
	body, afterEcho, bounded := tr.newestTurn(lines)
	text = tr.turnProse(body, afterEcho)
	// A prompt sent while the last turn was still being read leaves the
	// newest turn empty, and the answer the user is looking at is the one
	// above it. Cut the prompt row itself with it, or the same empty turn
	// comes back.
	if start := len(lines) - len(body); text == "" && start > 0 {
		body, afterEcho, bounded = tr.newestTurn(lines[:start-1])
		text = tr.turnProse(body, afterEcho)
	}
	return text, bounded && !fromPane, true
}

// paneAboveComposer is the pane without the composer its tool draws at the
// bottom: everything above the last row of the tool's own frame. It is the
// fallback for a tool whose activity region holds no content of its own.
func (tr toolRules) paneAboveComposer(pane string) []string {
	lines := strings.Split(pane, "\n")
	if tr.chromeLine == nil {
		return lines
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if tr.chromeLine.MatchString(strings.TrimRight(lines[i], " \t")) {
			return lines[:i]
		}
	}
	return lines
}

// turnProse is the reply inside one turn's rows: paragraph breaks kept,
// tool results and the tool's own frame dropped. afterEcho says a prompt
// sits above these rows, so the wrapped tail of it may still be among
// them; a completed turn can also hold an entirely unmarked reply.
func (tr toolRules) turnProse(body []string, afterEcho bool) string {
	// The reply's own opening marker beats guessing where the prompt
	// ended, so take it whenever the turn's start is in frame.
	if afterEcho {
		if marked := tr.firstMessageIndex(body); marked >= 0 {
			body, afterEcho = body[marked:], false
		}
	}
	// Indentation is all that marks a wrapped prompt's continuation rows,
	// and only a tool whose replies open on a marker can be read that
	// way: an unmarked reply here starts at the left edge.
	trimPrompt := afterEcho && tr.messageStart != nil
	out := tr.contentRows(body, trimPrompt)
	if len(out) == 0 && trimPrompt && tr.turnEnd != nil && tr.lastTurnEndIndex(body) >= 0 {
		out = tr.contentRows(body, false)
	}
	return strings.Join(out, "\n")
}

// contentRows is body without the tool's frame, its tool results and
// their wrapped rows, stopping at the summary that closes the turn.
// trimPrompt drops indented rows until the first row at the left edge.
func (tr toolRules) contentRows(body []string, trimPrompt bool) []string {
	out := make([]string, 0, len(body))
	inBlock := tr.chromeBlockRows(body)
	inResult := false
	for i, raw := range body {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		if inBlock[i] {
			continue
		}
		if tr.toolResult != nil && tr.toolResult.MatchString(line) {
			inResult = true
			continue
		}
		// A result runs past its own marker row onto the rows it wrapped
		// onto, which carry no marker of their own.
		if inResult && wrapsAbove(line) {
			continue
		}
		inResult = false
		if tr.isStructural(line) {
			// A turn_end closes the turn, so a notice printed below it
			// belongs to no reply.
			if tr.turnEnd != nil && tr.turnEnd.MatchString(line) {
				break
			}
			continue
		}
		if trimPrompt && len(out) == 0 && wrapsAbove(line) {
			continue
		}
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// newestTurn is the region past its newest prompt: the row after the
// prompt the tool echoed, or after the last composer row for a tool that
// echoes nothing (grok, hermes), whose cutoff draws every prompt it kept
// on screen. With no prompt in frame either way, the summary that closed
// the previous turn bounds it instead. afterEcho reports that a prompt
// was found, so the rows under it can still be its wrapped tail.
func (tr toolRules) newestTurn(lines []string) (body []string, afterEcho, bounded bool) {
	if tr.userEcho != nil {
		if i := tr.lastEchoIndex(lines); i >= 0 {
			return lines[i+1:], true, true
		}
	} else {
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimRight(lines[i], " \t")
			if tr.inputRow(line) && !tr.matchesAnyRule(line) {
				return lines[i+1:], true, true
			}
		}
	}
	if i := tr.previousTurnEndIndex(lines); i >= 0 {
		return lines[i+1:], false, true
	}
	return lines, false, false
}

// previousTurnEndIndex is the turn summary that closed the turn before
// the newest one, the bound left when no prompt is in frame: a tool that
// keeps none (grok), or a prompt the capture cut off or a status rule
// claimed. A running turn has drawn no summary of its own yet, so the
// last one is that boundary; once it ends, the last summary is its own
// and the one above it opens the turn. -1 when neither is in frame.
func (tr toolRules) previousTurnEndIndex(lines []string) int {
	if tr.turnEnd == nil {
		return -1
	}
	previous, last := -1, -1
	contentBelow := false
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if tr.turnEnd.MatchString(line) {
			previous, last, contentBelow = last, i, false
			continue
		}
		if tr.isContent(line) {
			contentBelow = true
		}
	}
	if contentBelow {
		return last
	}
	return previous
}

// firstMessageIndex is the first row opening a message the tool printed,
// or -1 for a turn that rendered none.
func (tr toolRules) firstMessageIndex(lines []string) int {
	if tr.messageStart == nil {
		return -1
	}
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if !tr.isContent(line) {
			continue
		}
		if tr.messageStart.MatchString(line) {
			return i
		}
	}
	return -1
}

// isContent reports whether a row carries something the agent wrote,
// rather than a blank, the tool's own frame, or a tool result.
func (tr toolRules) isContent(line string) bool {
	if strings.TrimSpace(line) == "" || tr.isStructural(line) {
		return false
	}
	return tr.toolResult == nil || !tr.toolResult.MatchString(line)
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
