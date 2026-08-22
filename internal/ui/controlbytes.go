package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
)

func isControlByte(c byte) bool { return c < 0x20 || c == 0x7f }

// needsEscaping also reports malformed UTF-8, because a byte that decodes to
// nothing is where a raw 8-bit CSI or OSC would hide.
func needsEscaping(text string) bool {
	for i := 0; i < len(text); i++ {
		if isControlByte(text[i]) {
			return true
		}
	}
	return !utf8.ValidString(text)
}

// escapeControls rewrites control bytes as caret notation, so an ESC reads as
// "^[", and a byte that is not part of well-formed UTF-8 as hex. Review shows
// code and paths the user did not write, and either kind left intact is obeyed
// by the terminal rather than shown: it can repaint the screen the live agent
// panes share, or set the window title. A lone 0x9b or 0x9d is the 8-bit CSI
// and OSC, and is malformed UTF-8, so it escapes as hex; the same code points
// encoded properly are ordinary text and stay whole, which is what keeps a
// Hebrew line (its continuation bytes land in that range) readable.
func escapeControls(text string) string {
	if !needsEscaping(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); {
		if c := text[i]; c < utf8.RuneSelf {
			if isControlByte(c) {
				b.WriteByte('^')
				b.WriteByte(c ^ 0x40)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, `\x%02X`, text[i])
			i++
			continue
		}
		b.WriteString(text[i : i+size])
		i += size
	}
	return b.String()
}

// escapeSpans moves word-diff byte offsets onto the escaped rendering, where a
// control byte now takes two bytes and a malformed one takes four.
func escapeSpans(text string, spans []diff.Span) []diff.Span {
	if len(spans) == 0 || !needsEscaping(text) {
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
			if isControlByte(c) {
				shift++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			shift += 3
		}
		i += size
	}
	return offset + shift
}
