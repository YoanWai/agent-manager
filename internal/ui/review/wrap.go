package review

import (
	"strings"
	"unicode/utf8"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

const (
	bgAdd     = "\x1b[48;2;22;42;22m"
	bgDel     = "\x1b[48;2;48;24;24m"
	bgAddSpan = "\x1b[48;2;38;74;38m"
	bgDelSpan = "\x1b[48;2;82;36;36m"
)

// wrapTinted overlays a diff background onto a chroma-highlighted line
// and wraps it to width, returning one entry per visual row. The diff
// background is re-emitted after every SGR reset chroma writes so it
// survives across tokens and across wrap boundaries; word spans (byte
// offsets into the raw text) switch to the brighter span background.
// When a background is set, each row is padded so the tint fills the
// full width; a plain line (empty baseBg) is left unpadded for the
// caller to pad. Every row is closed with a reset.
//
// Wrapping prefers word boundaries (space characters): when a non-space
// character would overflow the row, the current word moves to the next
// line instead of being split mid-identifier. A word wider than the line
// width falls back to character-level splitting.
func wrapTinted(highlighted string, spans []diff.Span, baseBg, spanBg string, width int) []string {
	if width < 1 {
		width = 1
	}
	bgFor := func(offset int) string {
		for _, span := range spans {
			if offset >= span.Start && offset < span.End {
				return spanBg
			}
		}
		return baseBg
	}

	type seg struct {
		text    string
		visible int
		bytes   int
	}
	var tokens []seg
	i := 0
	for i < len(highlighted) {
		if highlighted[i] == 0x1b {
			end := i + 1
			if end < len(highlighted) && highlighted[end] == '[' {
				end++
				for end < len(highlighted) && highlighted[end] != 'm' {
					end++
				}
				if end < len(highlighted) {
					end++
				}
			}
			tokens = append(tokens, seg{text: highlighted[i:end]})
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(highlighted[i:])
		if r == ' ' {
			tokens = append(tokens, seg{text: " ", visible: 1, bytes: 1})
			i++
			continue
		}
		end := i + size
		visible := ansi.StringWidth(string(r))
		bytes := size
		for end < len(highlighted) && highlighted[end] != ' ' && highlighted[end] != 0x1b {
			r2, s2 := utf8.DecodeRuneInString(highlighted[end:])
			visible += ansi.StringWidth(string(r2))
			bytes += s2
			end += s2
		}
		tokens = append(tokens, seg{text: highlighted[i:end], visible: visible, bytes: bytes})
		i = end
	}

	var rows []string
	var b strings.Builder
	activeBg := ""
	activeFg := ""
	rowWidth := 0
	fresh := true
	offset := 0

	noteAnsi := func(seq string) {
		if seq == "\x1b[0m" {
			activeFg = ""
		} else {
			activeFg += seq
		}
	}

	closeRow := func() {
		if baseBg != "" && rowWidth < width {
			b.WriteString(strings.Repeat(" ", width-rowWidth))
		}
		if b.Len() > 0 || baseBg != "" {
			b.WriteString("\x1b[0m")
		}
		rows = append(rows, b.String())
		b.Reset()
		fresh = true
		rowWidth = 0
	}

	emitText := func(text string, textBytes int) {
		for ti := 0; ti < len(text); {
			if text[ti] == 0x1b {
				end := ti + 1
				if end < len(text) && text[end] == '[' {
					end++
					for end < len(text) && text[end] != 'm' {
						end++
					}
					if end < len(text) {
						end++
					}
				}
				seq := text[ti:end]
				b.WriteString(seq)
				noteAnsi(seq)
				if seq == "\x1b[0m" && activeBg != "" {
					b.WriteString(activeBg)
				}
				ti = end
				continue
			}
			if fresh {
				activeBg = bgFor(offset)
				if activeBg != "" {
					b.WriteString(activeBg)
				}
				if activeFg != "" {
					b.WriteString(activeFg)
				}
				fresh = false
			}
			r, rSize := utf8.DecodeRuneInString(text[ti:])
			rWidth := ansi.StringWidth(string(r))
			if rowWidth+rWidth > width {
				closeRow()
				continue
			}
			if bg := bgFor(offset); bg != activeBg {
				activeBg = bg
				b.WriteString(bg)
			}
			b.WriteString(text[ti : ti+rSize])
			offset += rSize
			rowWidth += rWidth
			ti += rSize
		}
	}

	for ti := 0; ti < len(tokens); ti++ {
		tok := tokens[ti]
		isAnsi := tok.visible == 0 && tok.bytes == 0
		isSpace := tok.text == " "

		if isAnsi {
			b.WriteString(tok.text)
			noteAnsi(tok.text)
			if tok.text == "\x1b[0m" && activeBg != "" {
				b.WriteString(activeBg)
			}
			continue
		}

		if isSpace {
			if rowWidth+1 > width {
				closeRow()
				continue
			}
			if bg := bgFor(offset); bg != activeBg {
				activeBg = bg
				b.WriteString(bg)
			}
			b.WriteByte(' ')
			offset++
			rowWidth++
			continue
		}

		if rowWidth+tok.visible > width {
			if rowWidth > 0 {
				closeRow()
			}
			if tok.visible > width {
				emitText(tok.text, tok.bytes)
				continue
			}
		}
		emitText(tok.text, tok.bytes)
	}

	if !fresh || len(rows) == 0 {
		closeRow()
	}
	return rows
}

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
