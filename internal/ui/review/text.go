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

func SanitizeText(text string) string { return stripControls(text) }

func containsNUL(s string) bool { return strings.Contains(s, "\x00") }

// StripBaseHash drops the @<short-sha> suffix BaseDesc carries from
// baseRefFor. The hash matters for telling two merge-bases apart in logs
// but reads as noise where only the ref name carries signal.
func StripBaseHash(desc string) string {
	if i := strings.LastIndexByte(desc, '@'); i >= 0 {
		return desc[:i]
	}
	return desc
}
