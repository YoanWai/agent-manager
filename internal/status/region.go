package status

import (
	"github.com/charmbracelet/x/ansi"
	"regexp"
	"strings"
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

// RegionContent is an activity region without the rows chrome_line marks as
// the tool's own frame. A frame redrawing is not the agent at work: agy fills
// its header in a second after its composer is already up.
func (e *Engine) RegionContent(tool, region string) string {
	tr, ok := e.tools[tool]
	if !ok || tr.chromeLine == nil {
		return region
	}
	lines := strings.Split(region, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !tr.chromeLine.MatchString(strings.TrimRight(line, " \t")) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
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
		matchText := line
		if !open {
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimRight(lines[j], " \t")
				if strings.TrimSpace(next) != "" {
					matchText += "\n" + next
					break
				}
			}
		}
		open = open || tr.chromeBlock.MatchString(matchText)
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
