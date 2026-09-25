package status

import (
	"strings"
)

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
// -1 when the region holds none.
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
	for i := end - 1; i >= 0; i-- {
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
		return i
	}
	return -1
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

// ParksItsCaret reports whether the tool paints its own composer cursor
// and rests the terminal caret away from where typing lands, which is
// what declaring composer_placeholder means. The focus crop treats such a
// tool's caret on a blank bottom row as furniture rather than a typing
// point.
func (e *Engine) ParksItsCaret(tool string) bool {
	tr, ok := e.tools[tool]
	return ok && tr.composerPlaceholder != ""
}
