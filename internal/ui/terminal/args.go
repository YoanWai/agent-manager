package terminal

import (
	"strings"
	"unicode"
)

// splitArgs splits a $BROWSER entry into argv, grouping on single and double
// quotes so an argument can carry spaces. Quoting is all it borrows from a
// shell: the entry is a setting, not a program, so an unclosed quote runs to
// the end of the line.
func splitArgs(line string) []string {
	var argv []string
	var current strings.Builder
	quote := rune(0)
	quoted := false
	flush := func() {
		if current.Len() > 0 || quoted {
			argv = append(argv, current.String())
			current.Reset()
			quoted = false
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote, quoted = r, true
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return argv
}
