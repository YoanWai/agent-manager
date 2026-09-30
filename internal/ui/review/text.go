package review

import (
	"strings"
	"unicode"
)

func stripControls(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, text)
}

func containsNUL(s string) bool { return strings.Contains(s, "\x00") }
