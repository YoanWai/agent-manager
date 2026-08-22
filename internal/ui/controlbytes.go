package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/diff"
)

func isControlByte(c byte) bool { return c < 0x20 || c == 0x7f }

func hasControlBytes(text string) bool {
	for i := 0; i < len(text); i++ {
		if isControlByte(text[i]) {
			return true
		}
	}
	return false
}

// escapeControls rewrites control bytes as caret notation, so an ESC reads as
// "^[". Review shows code and paths the user did not write, and a control byte
// left intact there is obeyed by the terminal rather than shown: it can repaint
// the screen the live agent panes share, or set the window title. Bytes above
// ASCII pass through, which leaves multi-byte UTF-8 whole.
func escapeControls(text string) string {
	if !hasControlBytes(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if isControlByte(c) {
			b.WriteByte('^')
			b.WriteByte(c ^ 0x40)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// escapeSpans moves word-diff byte offsets onto the escaped rendering, where
// every control byte now takes two bytes instead of one.
func escapeSpans(text string, spans []diff.Span) []diff.Span {
	if len(spans) == 0 || !hasControlBytes(text) {
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
	for i := 0; i < offset && i < len(text); i++ {
		if isControlByte(text[i]) {
			shift++
		}
	}
	return offset + shift
}
