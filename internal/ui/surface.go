package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The list view is built from painted surfaces rather than drawn boxes:
// the app fills a backdrop, the sessions rail sits on a slightly lifted
// panel tone, and the selected entry lifts once more. Depth carries the
// structure; a hairline marks each seam where two surfaces meet.

// backdropHex is the tone behind the whole frame.
func backdropHex() string { return current.Bg }

// panelHex is the sessions rail: one subtle step above the backdrop.
func panelHex() string { return mix(current.Bg, current.Surface, 0.34) }

// blockHex is a section block inside the content area, quieter than the
// rail so it groups without announcing itself.
func blockHex() string { return mix(current.Bg, current.Surface, 0.18) }

// selectedHex is the band under the cursor's entry.
func selectedHex() string { return mix(current.Bg, current.Surface, 0.72) }

// ruleHex is the hairline tone: lifted just far enough off the backdrop to
// draw a seam without becoming a border.
func ruleHex() string { return mix(current.Bg, current.Text, 0.17) }

// hrule is a horizontal seam across a painted row.
func hrule(width int) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ruleHex())).Render(strings.Repeat("─", width))
}

// vruleColumn is the seam between two painted columns. It doubles as the
// resize grip, taking the accent while the divider is being moved.
func (m *Model) vruleColumn(height int) []string {
	color := lipgloss.Color(ruleHex())
	glyph := "│"
	switch {
	case m.splitDragging:
		color, glyph = colorAccent2, "║"
	case m.resizeMode:
		color, glyph = colorAccent, "║"
	}
	cell := lipgloss.NewStyle().Foreground(color).Render(glyph)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = paint(cell, 1, panelHex())
	}
	return lines
}

// paint pads a possibly-styled line to an exact width and fills every cell
// with bg. Inner SGR resets emitted by per-segment renders would drop the
// fill partway across, so each reset re-applies it.
func paint(s string, width int, bg string) string {
	fill := bgSeq(bg)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+fill)
	if w := ansi.StringWidth(s); w > width {
		s = ansi.Truncate(s, width, "…")
	} else if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return fill + s + "\x1b[0m"
}

// paintRows paints each line of a block, padding the block itself out to
// height so a short column still fills its side of the frame.
func paintRows(lines []string, width, height int, bg string) []string {
	out := make([]string, height)
	for i := 0; i < height; i++ {
		content := ""
		if i < len(lines) {
			content = lines[i]
		}
		out[i] = paint(content, width, bg)
	}
	return out
}

// joinColumns stitches painted columns row by row.
func joinColumns(columns ...[]string) []string {
	height := 0
	for _, col := range columns {
		if len(col) > height {
			height = len(col)
		}
	}
	rows := make([]string, height)
	for i := 0; i < height; i++ {
		var b strings.Builder
		for _, col := range columns {
			if i < len(col) {
				b.WriteString(col[i])
			}
		}
		rows[i] = b.String()
	}
	return rows
}

// indentLines insets a block by n columns.
func indentLines(lines []string, n int) []string {
	pad := strings.Repeat(" ", n)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = pad + line
	}
	return out
}

// splitLines splits a rendered block into lines, treating the empty string
// as no lines rather than one blank line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
