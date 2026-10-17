// Package presentation holds the pure text helpers every UI package shares:
// the row marks in both symbol modes, width-aware padding, truncation and
// centering, and the control glyphs. It imports nothing from the module.
package presentation

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// PadRight pads or clips a possibly-styled string to an exact display width.
func PadRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	if strings.ContainsRune(s, 0x1b) {
		s += "\x1b[0m"
	}
	return s
}

// Divider renders a labeled section rule with styles supplied by its caller.
func Divider(label string, width int, section, rule lipgloss.Style) string {
	head := section.Render("▍"+label) + " "
	dashes := width - ansi.StringWidth(label) - 2
	if dashes < 0 {
		dashes = 0
	}
	return head + rule.Render(strings.Repeat("─", dashes))
}

// Window returns a bounded slice and marks content hidden above or below it.
func Window(lines []string, rows, offset int, overflow lipgloss.Style) []string {
	if rows < 1 {
		rows = 1
	}
	if rows >= len(lines) {
		return lines
	}
	maxOffset := max(0, len(lines)-rows)
	offset = min(max(offset, 0), maxOffset)
	window := append([]string(nil), lines[offset:min(offset+rows, len(lines))]...)
	above := offset > 0
	below := offset+rows < len(lines)
	if len(window) == 1 && above && below {
		window[0] = overflow.Render("↕ more…")
		return window
	}
	if above {
		window[0] = overflow.Render("↑ more above…")
	}
	if below {
		window[len(window)-1] = overflow.Render("↓ more below…")
	}
	return window
}

// Follow is the first row of a window visible rows tall over count rows
// that keeps cursor on screen. It holds at first while the cursor stays
// inside and scrolls just far enough when it leaves, so the row under the
// pointer holds still between two clicks.
func Follow(first, cursor, count, visible int) int {
	if visible >= count {
		return 0
	}
	switch {
	case cursor < first:
		first = cursor
	case cursor >= first+visible:
		first = cursor - visible + 1
	}
	return min(max(first, 0), count-visible)
}

// TruncateTail keeps the end of the string, best for paths and URLs.
func TruncateTail(s string, max int) string {
	runes := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-max+1:])
}

// CenterLine centers text inside width, clipping it when it is wider.
func CenterLine(text string, width int) string {
	if width <= 0 {
		return text
	}
	visible := ansi.StringWidth(text)
	if visible >= width {
		return ansi.Truncate(text, width, "…")
	}
	return strings.Repeat(" ", (width-visible)/2) + text
}

// TruncatePath keeps the end of a path and cuts at a separator when one is near.
func TruncatePath(path string, limit int) string {
	runes := []rune(path)
	if limit <= 0 {
		return ""
	}
	if len(runes) <= limit {
		return path
	}
	if limit == 1 {
		return "…"
	}
	tail := string(runes[len(runes)-limit+1:])
	if i := strings.IndexByte(tail, '/'); i >= 0 && i < len(tail)-1 {
		return "…" + tail[i:]
	}
	return "…" + tail
}

// PadToHeight appends blank rows until the text is height rows tall.
func PadToHeight(s string, height int) string {
	missing := height - lipgloss.Height(s)
	if missing > 0 {
		s += strings.Repeat("\n", missing)
	}
	return s
}
