package status

import (
	"github.com/charmbracelet/x/ansi"

	"unicode"
)

func HasTextBeforeCaret(engine *Engine, tool, row string, caretX int) bool {
	prefix, ok := engine.InputPrefix(tool, row)
	if !ok {
		return false
	}
	line := []rune(row)
	for cell := ansi.StringWidth(prefix); cell < caretX; {
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
