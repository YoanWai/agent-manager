package status

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func HasTextBeforeCaret(engine *Engine, tool, row string, caretX int) bool {
	prefix, ok := engine.InputPrefix(tool, row)
	if !ok {
		return false
	}
	return textBetween(row, ansi.StringWidth(prefix), caretX)
}

// HasDraftBeforeCaret is HasTextBeforeCaret for a draft over the rows between the caret and the marker row.
func HasDraftBeforeCaret(engine *Engine, tool string, rows []string, caretX, caretY int) bool {
	row := rows[caretY]
	if _, ok := engine.InputPrefix(tool, row); ok {
		return HasTextBeforeCaret(engine, tool, row, caretX)
	}
	// cursor and command-code park the caret below everything they draw.
	if blankRows(rows[caretY:]) {
		return false
	}
	chrome := engine.tools[tool].chromeLine
	drafted := textBetween(row, 0, caretX)
	for y := caretY - 1; y >= 0; y-- {
		above := rows[y]
		if prefix, ok := engine.InputPrefix(tool, above); ok {
			return drafted || strings.TrimSpace(above[len(prefix):]) != ""
		}
		if strings.TrimSpace(above) == "" {
			continue
		}
		if engine.MatchesActivityCutoff(tool, above) || (chrome != nil && chrome.MatchString(strings.TrimRight(above, " \t"))) {
			return false
		}
		drafted = true
	}
	return false
}

func blankRows(rows []string) bool {
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			return false
		}
	}
	return true
}

func textBetween(row string, from, caretX int) bool {
	line := []rune(row)
	for cell := from; cell < caretX; {
		index := RuneAtColumn(line, cell)
		if index >= len(line) {
			return false
		}
		if !unicode.IsSpace(line[index]) {
			return true
		}
		cell += ansi.StringWidth(string(line[index]))
	}
	return false
}

func RuneAtColumn(line []rune, column int) int {
	cell := 0
	for i, r := range line {
		next := cell + ansi.StringWidth(string(r))
		if column < next {
			return i
		}
		cell = next
	}
	return len(line)
}

// PromptRules binds the engine's prompt rules to one tool, the shape the
// arrow step's prompt-head check reads them in.
type PromptRules struct {
	Engine *Engine
	Tool   string
}

func (r PromptRules) InputPrefix(row string) (string, bool) {
	return r.Engine.InputPrefix(r.Tool, row)
}

func (r PromptRules) TextBeforeCaret(row string, caretX int) bool {
	return HasTextBeforeCaret(r.Engine, r.Tool, row, caretX)
}

func (r PromptRules) ComposerIsEmpty(row string) bool {
	return r.Engine.ComposerIsEmpty(r.Tool, row)
}

func (r PromptRules) MatchesActivityCutoff(row string) bool {
	return r.Engine.MatchesActivityCutoff(r.Tool, row)
}

func (r PromptRules) ParksItsCaret() bool { return r.Engine.ParksItsCaret(r.Tool) }
