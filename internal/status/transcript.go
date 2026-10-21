package status

import (
	"slices"
	"strings"
)

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
	i := tr.lastEchoIndex(lines)
	if i < 0 {
		return "", true
	}
	line := strings.TrimRight(lines[i], " \t")
	loc := tr.userEcho.FindStringIndex(line)
	return strings.TrimSpace(line[loc[1]:]), true
}

// lastEchoIndex is the row carrying the newest prompt the tool echoed, or
// -1 when the region holds none: the lowest echo row, except for a tool
// whose own rows share the prompt's marker (tool_row), where a prompt
// opens its turn and the newest one is the first echo under the last
// summary row that has one below it. A tool_row opens a block of the
// tool's own rows (a command and then its output), which runs to the
// next row outside the gutter.
func (tr toolRules) lastEchoIndex(lines []string) int {
	if tr.userEcho == nil {
		return -1
	}
	end := len(lines)
	// A composer drawn above the cutoff (opencode's ┃ gutter) is a run of
	// input_prefix rows hugging the region's end; the echoes live higher,
	// so the trailing run is the composer's, not a message.
	if tr.inputPrefix != nil {
		for end > 0 {
			last := lines[end-1]
			if strings.TrimSpace(last) == "" || tr.inputPrefix.MatchString(last) {
				end--
				continue
			}
			break
		}
	}
	if tr.turnEnd == nil || tr.toolRow == nil {
		for i := end - 1; i >= 0; i-- {
			if tr.echoRow(strings.TrimRight(lines[i], " \t")) {
				return i
			}
		}
		return -1
	}
	found, current := -1, -1
	toolBlock := false
	for i := 0; i < end; i++ {
		line := strings.TrimRight(lines[i], " \t")
		if tr.turnEnd.MatchString(line) {
			if current >= 0 {
				found = current
			}
			current, toolBlock = -1, false
			continue
		}
		switch {
		case tr.toolRow.MatchString(line):
			toolBlock = true
		case strings.TrimSpace(line) != "" && !tr.inputPrefix.MatchString(line):
			toolBlock = false
		}
		if current < 0 && !toolBlock && tr.echoRow(line) {
			current = i
		}
	}
	if current >= 0 {
		return current
	}
	return found
}

// echoRow reports a row that carries an echoed prompt. A dialog draws its
// option rows behind the same marker the composer uses (codex's "› 1.
// Yes, continue"), so a line any status rule recognises is the tool's
// frame, not an echo, and so is a tool_row.
func (tr toolRules) echoRow(line string) bool {
	loc := tr.userEcho.FindStringIndex(line)
	if loc == nil || tr.matchesAnyRule(line) {
		return false
	}
	if tr.toolRow != nil && tr.toolRow.MatchString(line) {
		return false
	}
	echoed := strings.TrimSpace(line[loc[1]:])
	if echoed == "" {
		return false
	}
	return tr.placeholder == nil || !tr.placeholder.MatchString(echoed)
}

func (tr toolRules) matchesAnyRule(line string) bool {
	for _, r := range tr.rules {
		if r.re.MatchString(line) {
			return true
		}
	}
	return false
}
