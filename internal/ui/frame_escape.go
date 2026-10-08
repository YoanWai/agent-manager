package ui

import (
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

func escapeControls(text string) string       { return presentation.EscapeControls(text) }
func escapeControlsInline(text string) string { return presentation.EscapeControlsInline(text) }

// escapeSpans moves word-diff byte offsets onto the escaped rendering, where a
// control byte now takes two bytes, a malformed one four, and a C1 rune six.
func escapeSpans(text string, spans []diff.Span) []diff.Span {
	if len(spans) == 0 || !presentation.NeedsEscaping(text) {
		return spans
	}
	moved := make([]diff.Span, len(spans))
	for i, span := range spans {
		moved[i] = diff.Span{Start: escapedOffset(text, span.Start), End: escapedOffset(text, span.End)}
	}
	return moved
}

func escapedOffset(text string, offset int) int {
	shift := 0
	for i := 0; i < offset && i < len(text); {
		if c := text[i]; c < utf8.RuneSelf {
			if presentation.IsControlByte(c) && c != '\n' {
				shift++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			shift += 3
		case presentation.IsEscapedRune(r):
			shift += 6 - size
		}
		i += size
	}
	return offset + shift
}
