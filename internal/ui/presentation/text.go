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
