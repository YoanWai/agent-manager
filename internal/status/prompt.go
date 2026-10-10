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

// HasDraftBeforeCaret is HasTextBeforeCaret for a draft that may wrap: a
// tool that draws its marker on the first composer row only (omp) leaves the
// caret on a markerless row once the draft wraps. Such a row holds a draft
// when it has text before the caret and the unbroken rows above it lead to a
// marker row that carries text too.
func HasDraftBeforeCaret(engine *Engine, tool string, rows []string, caretX, caretY int) bool {
	row := rows[caretY]
	if _, ok := engine.InputPrefix(tool, row); ok {
		return HasTextBeforeCaret(engine, tool, row, caretX)
	}
	if !textBetween(row, 0, caretX) {
		return false
	}
	for y := caretY - 1; y >= 0; y-- {
		above := rows[y]
		if prefix, ok := engine.InputPrefix(tool, above); ok {
			return strings.TrimSpace(above[len(prefix):]) != ""
		}
		if strings.TrimSpace(above) == "" || engine.MatchesActivityCutoff(tool, above) {
			return false
		}
	}
	return false
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
