package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The list view sits on the terminal's own background, with the sessions
// rail filling its pane flush — header rule to footer rule, window edge to
// seam — and the selected entry lifted once more. The backdrop itself is
// never painted: the terminal background sync keeps the terminal's default
// at the theme's tone, so the window padding around the cell grid carries
// the same color as the unpainted cells and the frame meets the window
// without a ring of a different tone.

// backdropHex is the backdrop's fill: none. paint treats it as "pad, but
// leave the terminal's background alone".
func backdropHex() string { return "" }

// panelHex is the rail's fill: one step above the backdrop.
func panelHex() string { return mix(current.Bg, current.Surface, 0.55) }

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

// boundedRuleRow is the row that opens or closes the body. Over the pane
// it draws half blocks in the pane's own tone — the fill bleeding half a
// cell past its box, which is as far as a character cell can be divided —
// and across the content it draws the thin rule, whose center line meets
// the half blocks' edge at the same height.
// The run's interior is drawn inverted — the cell background carries the
// pane tone and the glyph paints the backdrop half in the theme's backdrop
// color — but both end cells are not. The terminal extends a row's edge
// cell background into the window margin at full cell height, so an
// inverted first cell smears pane tone past the pane's corner; drawing it
// as a foreground half block keeps the margin on the terminal's own
// background. The last cell sits over the bleed column, whose fill is only
// half a cell wide, so it takes a quadrant instead of a half block and the
// pane tone stops at the corner instead of jutting past it.
func (m *Model) boundedRuleRow(paneWidth, width int, edge string) string {
	if paneWidth < 2 || paneWidth >= width {
		return paint(hrule(width), width, backdropHex())
	}
	facing, corner := "▄", "▜"
	if edge == "▄" {
		facing, corner = "▀", "▟"
	}
	first := plain(lipgloss.NewStyle().Foreground(lipgloss.Color(panelHex())).Render(facing), 1)
	interior := lipgloss.NewStyle().Foreground(colorBg).
		Render(strings.Repeat(edge, paneWidth-1))
	last := lipgloss.NewStyle().Foreground(colorBg).Render(corner)
	return first + paint(interior, paneWidth-1, panelHex()) + paint(last, 1, panelHex()) +
		paint(hrule(width-paneWidth-1), width-paneWidth-1, backdropHex())
}

// railEdgeCell is one row of the pane's first column, drawn as a foreground
// block in the row's own tone rather than as cell background. The window
// margin beside it inherits the cell's background — the terminal's own —
// so the pane's left edge lands exactly on the cell grid.
func railEdgeCell(tone string) string {
	return plain(lipgloss.NewStyle().Foreground(lipgloss.Color(tone)).Render("█"), 1)
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

// bleedColumn finishes a pane's right edge: a half block in the pane's
// tone on the backdrop, extending the fill half a cell past the seam.
func (m *Model) bleedColumn(height int) []string {
	cell := paint(lipgloss.NewStyle().Foreground(colorBg).Render("▐"), 1, panelHex())
	lines := make([]string, height)
	for i := range lines {
		lines[i] = cell
	}
	return lines
}

// seamCell is one row of the vertical seam. A rule arriving from either
// side turns the cell into the matching junction, so crossing lines meet
// instead of butting against each other; while the divider is being moved
// the whole seam becomes the grip and junctions give way to it.
func (m *Model) seamCell(leftRule, rightRule bool) string {
	// While the divider is being moved the seam becomes the grip.
	if m.splitDragging || m.resizeMode {
		color := colorAccent
		if m.splitDragging {
			color = colorAccent2
		}
		return paint(lipgloss.NewStyle().Foreground(color).Render("║"), 1, panelHex())
	}
	// Otherwise the seam is the pane's own fill — its edge is drawn by the
	// bleed column beside it, not by a line — and a rule arriving from
	// either side crosses it as a rule cell on that fill.
	if leftRule || rightRule {
		return paint(hrule(1), 1, panelHex())
	}
	return paint("", 1, panelHex())
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
// terminal's own backdrop. Rail rows also carry the tone their fill uses,
// so the edge column beside them can match the selected entry's band.
type contentLine struct {
	text string
	raw  bool
	rule bool
	tone string
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
