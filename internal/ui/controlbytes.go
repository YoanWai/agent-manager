package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
)

func isControlByte(c byte) bool { return c < 0x20 || c == 0x7f }

// isEscapedRune reports the C1 controls, which reach a pane as well-formed
// UTF-8 and would otherwise pass through whole. U+009B is CSI and U+009D is
// OSC; beyond what a terminal that reads them does with them, a C1 rune paints
// a cell that ansi.StringWidth does not count, so a row carrying one runs past
// the seam it was measured for.
func isEscapedRune(r rune) bool { return r >= 0x80 && r <= 0x9f }

// needsEscaping also reports malformed UTF-8, because a byte that decodes to
// nothing is where a raw 8-bit CSI or OSC would hide.
func needsEscaping(text string) bool {
	for i := 0; i < len(text); {
		if c := text[i]; c < utf8.RuneSelf {
			if isControlByte(c) && c != '\n' {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if (r == utf8.RuneError && size == 1) || isEscapedRune(r) {
			return true
		}
		i += size
	}
	return false
}

// escapeControls rewrites control bytes as caret notation, so an ESC reads as
// "^[", a byte that is not part of well-formed UTF-8 as hex, and a C1 control
// as its code point. Review shows code and paths the user did not write, and
// any of those left intact is obeyed by the terminal rather than shown: it can
// repaint the screen the live agent panes share, or set the window title. A
// lone 0x9b or 0x9d is the 8-bit CSI and OSC and is malformed UTF-8, so it
// escapes as hex, while the same code points encoded properly escape as their
// code point. Hebrew survives either way: its continuation bytes land in that
// range, but the runes they build decode to U+05D0 and up.
//
// Newlines are kept. A multi-line git error is the shape this renders most
// often, and folding it into one row leaves paint to truncate everything past
// the first line; a surface that owns a single row calls escapeControlsInline.
func escapeControls(text string) string {
	if !needsEscaping(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); {
		if c := text[i]; c < utf8.RuneSelf {
			if isControlByte(c) && c != '\n' {
				b.WriteByte('^')
				b.WriteByte(c ^ 0x40)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02X`, text[i])
			i++
		case isEscapedRune(r):
			fmt.Fprintf(&b, `\u%04X`, r)
			i += size
		default:
			b.WriteString(text[i : i+size])
			i += size
		}
	}
	return b.String()
}

// escapeControlsInline is escapeControls for a surface that owns exactly one
// row: a header pill, a file name, a status line. It flattens the newlines
// escapeControls keeps, since a second line there pushes the layout around it.
func escapeControlsInline(text string) string {
	return strings.ReplaceAll(escapeControls(text), "\n", " ")
}

// escapeSpans moves word-diff byte offsets onto the escaped rendering, where a
// control byte now takes two bytes, a malformed one four, and a C1 rune six.
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
			if isControlByte(c) && c != '\n' {
				shift++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			shift += 3
		case isEscapedRune(r):
			shift += 6 - size
		}
		i += size
	}
	return offset + shift
}
