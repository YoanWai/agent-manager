package focus

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// PromptRules are the focused tool's prompt rules, which the root resolves
// from its status engine.
type PromptRules interface {
	// InputPrefix is the prompt marker a row opens with, when it is an input
	// row.
	InputPrefix(row string) (prefix string, ok bool)
	// TextBeforeCaret reports whether anything but blanks sits between a
	// tool's prompt marker and the caret on this row, which is how a line
	// someone has half written is told from an empty prompt. A row that is
	// not an input line carries no such text. Claude pads its marker with a
	// non-breaking space, so blank means any space rune, not the ASCII one
	// alone; tmux trims a row's trailing blanks, so a row that ends before
	// the caret is blank the rest of the way.
	TextBeforeCaret(row string, caretX int) bool
	ComposerIsEmpty(row string) bool
	MatchesActivityCutoff(row string) bool
	ParksItsCaret() bool
}

// CaretContext is what the root resolves for one prompt-head question:
// the session asked about, the pane mirror's text, whether that frame shows
// every write queued to the pane, and the tool's prompt rules.
type CaretContext struct {
	SessionID string
	Preview   string
	CaughtUp  bool
	Rules     PromptRules
}

// CaretAtInputStart reports whether the agent's caret sits at the head of
// its prompt, with nothing but the prompt marker to its left. Left is a
// no-op for the agent there, which is what frees the key to mean "back to
// the list" without ever costing a keystroke inside the prompt: anywhere
// else it still moves the caret.
//
// A wrapped prompt's continuation rows carry no marker, so a caret at the
// head of one of them forwards Left as usual and reaches the end of the
// row above.
//
// A tool may park the terminal cursor below its footer and paint its own
// block cursor inside the composer instead (command-code does). For one
// declaring composer_placeholder, a caret on a blank row reads the
// composer row instead: the placeholder being on screen is the evidence
// the composer is empty and its caret at the head of the prompt; a draft
// replaces the placeholder and Left belongs to the agent.
//
// A zero-width input_prefix declares a bare composer row (pi's). Its
// painted cursor is the only thing locating that row, so tmux's position
// stays meaningful when the application hides the terminal cursor.
func (m Model) CaretAtInputStart(ctx CaretContext) bool {
	pane := m.Pane()
	if ctx.Rules == nil || pane.SessionID != ctx.SessionID || m.ScrolledBack() {
		return false
	}
	// A frame taken before keys still on their way shows the caret where
	// typing was, not where it is.
	if !ctx.CaughtUp {
		return false
	}
	_, bareInput := ctx.Rules.InputPrefix("")
	caretCellKnown := pane.Cursor.Visible ||
		(pane.Cursor.PositionKnown && (ctx.Rules.ParksItsCaret() || bareInput))
	if !caretCellKnown {
		return false
	}
	rows := strings.Split(strings.TrimSuffix(ctx.Preview, "\n"), "\n")
	if pane.Cursor.Y < 0 || pane.Cursor.Y >= len(rows) {
		return false
	}
	row := ansi.Strip(rows[pane.Cursor.Y])
	prefix, ok := ctx.Rules.InputPrefix(row)
	if !ok {
		return caretParksAndComposerIsEmpty(ctx.Rules, rows, pane.Cursor)
	}
	if !ctx.Rules.TextBeforeCaret(row, pane.Cursor.X) &&
		pane.Cursor.X >= ansi.StringWidth(prefix) {
		return caretRowEndsAPromptHead(ctx.Rules, rows, pane.Cursor.Y)
	}
	return false
}

// caretParksAndComposerIsEmpty serves tools that park the terminal cursor
// below their footer and paint the composer's caret themselves. The parked
// cell sits on a blank row, so the composer is found by searching up for
// the nearest marker row, and an empty composer there is what proves Left
// costs the agent nothing: a draft holds the key instead.
// A caret cell that is not parked on a blank row is none of this path's
// business: the marker rules decide it as usual.
func caretParksAndComposerIsEmpty(rules PromptRules, rows []string, cursor Cursor) bool {
	// The parking spot is a blank corner cell: column zero on a row with
	// nothing painted on it. A cursor at column zero over any other
	// content is not the park, whatever sits above it.
	if cursor.X != 0 || strings.TrimSpace(ansi.Strip(rows[cursor.Y])) != "" {
		return false
	}
	for y := cursor.Y - 1; y >= 0; y-- {
		row := ansi.Strip(rows[y])
		if _, ok := rules.InputPrefix(row); !ok {
			continue
		}
		return rules.ComposerIsEmpty(row)
	}
	return false
}

// caretRowEndsAPromptHead rejects the row when a draft continues onto it:
// a multi-line prompt's blank continuation line looks exactly like an empty
// composer, and Left there belongs to the agent. The row above tells them
// apart when it carries the same marker with text past it. A wrapped line
// is rejected the same way; the rule that bounds the input box (pi's rule)
// is not draft text and does not reject.
func caretRowEndsAPromptHead(rules PromptRules, rows []string, y int) bool {
	if y == 0 {
		return true
	}
	above := ansi.Strip(rows[y-1])
	abovePrefix, ok := rules.InputPrefix(above)
	if !ok || rules.MatchesActivityCutoff(above) {
		return true
	}
	return strings.TrimSpace(above[len(abovePrefix):]) == ""
}

// CaretRow is the pane row the crop keeps in view for the caret, or -1 when
// there is none to keep: a hidden caret, a scrolled-back pane, or a tool's
// terminal cursor parked below its own painted composer.
func (m Model) CaretRow(preview string, parksItsCaret bool) int {
	pane := m.Pane()
	if !pane.Cursor.Visible || m.ScrolledBack() {
		return -1
	}
	caret := pane.Cursor.Y
	if parksItsCaret {
		rows := strings.Split(strings.TrimSuffix(preview, "\n"), "\n")
		if caret < len(rows) && caretParkedBelowContent(rows, caret, pane.Cursor.X) {
			return -1
		}
	}
	return caret
}

func caretParkedBelowContent(rows []string, caret, column int) bool {
	if column != 0 {
		return false
	}
	for y := caret; y < len(rows); y++ {
		if strings.TrimSpace(ansi.Strip(rows[y])) != "" {
			return false
		}
	}
	for y := caret - 1; y >= 0; y-- {
		if strings.TrimSpace(ansi.Strip(rows[y])) != "" {
			return true
		}
	}
	return false
}
