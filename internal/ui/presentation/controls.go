package presentation

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func IsControlByte(c byte) bool { return c < 0x20 || c == 0x7f }

// IsEscapedRune reports the C1 controls, which reach a pane as well-formed
// UTF-8 and would otherwise pass through whole. U+009B is CSI and U+009D is
// OSC; beyond what a terminal that reads them does with them, a C1 rune paints
// a cell that ansi.StringWidth does not count, so a row carrying one runs past
// the seam it was measured for.
func IsEscapedRune(r rune) bool { return r >= 0x80 && r <= 0x9f }

// NeedsEscaping also reports malformed UTF-8, because a byte that decodes to
// nothing is where a raw 8-bit CSI or OSC would hide.
func NeedsEscaping(text string) bool {
	for i := 0; i < len(text); {
		if c := text[i]; c < utf8.RuneSelf {
			if IsControlByte(c) && c != '\n' {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if (r == utf8.RuneError && size == 1) || IsEscapedRune(r) {
			return true
		}
		i += size
	}
	return false
}

// EscapeControls rewrites control bytes as caret notation, so an ESC reads as
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
// the first line; a surface that owns a single row calls EscapeControlsInline.
func EscapeControls(text string) string {
	if !NeedsEscaping(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); {
		if c := text[i]; c < utf8.RuneSelf {
			if IsControlByte(c) && c != '\n' {
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
		case IsEscapedRune(r):
			fmt.Fprintf(&b, `\u%04X`, r)
			i += size
		default:
			b.WriteString(text[i : i+size])
			i += size
		}
	}
	return b.String()
}

// EscapeControlsInline is EscapeControls for a surface that owns exactly one
// row: a header pill, a file name, a status line. It flattens the newlines
// EscapeControls keeps, since a second line there pushes the layout around it.
func EscapeControlsInline(text string) string {
	return strings.ReplaceAll(EscapeControls(text), "\n", " ")
}
