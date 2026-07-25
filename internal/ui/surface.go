package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The list view is built on the terminal's own background everywhere: the
// only filled cells are the selection band, chips and modal cards — small
// shapes that read as deliberate highlights and never touch a window edge.
// Structure comes from the seams and from type weight instead of from
// area fills, because a terminal blends window padding and transparency
// into its default background, and any large painted region sits beside
// that blend as an obviously different tone, like a stray child element
// carrying the background its container should have.

// backdropHex is the backdrop's fill: none. paint treats it as "pad, but
// leave the terminal's background alone".
func backdropHex() string { return "" }

// panelHex is the sessions rail's fill: none, same reasoning as the
// backdrop — the rail spans the window's whole left edge.
func panelHex() string { return "" }

// blockHex is a section block inside the content area, quieter than the
// rail so it groups without announcing itself.
func blockHex() string { return mix(current.Bg, current.Surface, 0.35) }

// selectedHex is the band under the cursor's entry.
func selectedHex() string { return current.Surface }

// ruleHex is the hairline tone: lifted just far enough off the backdrop to
// draw a seam without becoming a border.
func ruleHex() string { return mix(current.Bg, current.Text, 0.22) }

// hrule is a horizontal seam across a painted row.
func hrule(width int) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ruleHex())).Render(strings.Repeat("─", width))
}

// hruleJoined is a horizontal seam that meets the vertical one at column x,
// carrying the junction glyph so the two lines connect instead of crossing.
func hruleJoined(width, x int, junction string) string {
	if width < 1 {
		return ""
	}
	if x < 0 || x >= width {
		return hrule(width)
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(ruleHex()))
	return style.Render(strings.Repeat("─", x) + junction + strings.Repeat("─", width-x-1))
}

// vruleColumn is the seam between two painted columns. It doubles as the
// resize grip, taking the accent while the divider is being moved.
func (m *Model) vruleColumn(height int) []string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = m.seamCell(false, false)
	}
	return lines
}

// seamCell is one row of the vertical seam. A rule arriving from either
// side turns the cell into the matching junction, so crossing lines meet
// instead of butting against each other; while the divider is being moved
// the whole seam becomes the grip and junctions give way to it.
func (m *Model) seamCell(leftRule, rightRule bool) string {
	color := lipgloss.Color(ruleHex())
	glyph := "│"
	switch {
	case m.splitDragging:
		color, glyph = colorAccent2, "║"
	case m.resizeMode:
		color, glyph = colorAccent, "║"
	case leftRule && rightRule:
		glyph = "┼"
	case leftRule:
		glyph = "┤"
	case rightRule:
		glyph = "├"
	}
	return paint(lipgloss.NewStyle().Foreground(color).Render(glyph), 1, backdropHex())
}

// paint pads a possibly-styled line to an exact width and fills every cell
// with bg. Inner SGR resets emitted by per-segment renders would drop the
// fill partway across, so each reset re-applies it.
func paint(s string, width int, bg string) string {
	if bg == "" {
		return plain(s, width)
	}
	fill := bgSeq(bg)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+fill)
	if w := ansi.StringWidth(s); w > width {
		s = ansi.Truncate(s, width, "…")
	} else if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return fill + s + "\x1b[0m"
}

// plain pads a line to width without filling it, leaving the terminal's own
// background showing through. Captured agent output is drawn this way so a
// session's CLI looks exactly as it does inside the session.
func plain(s string, width int) string {
	if w := ansi.StringWidth(s); w > width {
		s = ansi.Truncate(s, width, "")
	} else if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return "\x1b[0m" + s + "\x1b[0m"
}

// contentLine is one row of the content column: ours to paint, a seam that
// spans the column edge to edge, or captured output that must keep the
// terminal's own backdrop.
type contentLine struct {
	text string
	raw  bool
	rule bool
}

// paintContent paints a content column, leaving raw rows unfilled.
func paintContent(lines []contentLine, width, height int, bg string) []string {
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i >= len(lines) {
			out[i] = paint("", width, bg)
			continue
		}
		if lines[i].rule {
			// Seams are drawn at the column's full width, not the inset the
			// text uses, so they meet the frame's edges exactly.
			out[i] = paint(hrule(width), width, bg)
			continue
		}
		if lines[i].raw {
			out[i] = plain(lines[i].text, width)
			continue
		}
		out[i] = paint(lines[i].text, width, bg)
	}
	return out
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
