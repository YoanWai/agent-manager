package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"

	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// railTopRow opens the body a third of a cell under the wordmark. Sextants,
// not half blocks: no half block fits the corner over the half-width bleed
// column at the height of the run beside it, so that cell always stepped.
func (m *Model) railTopRow(paneWidth, width int) string {
	if m.prefs.glyphMode.Effective() == presentation.GlyphModeBasic || paneWidth < 2 || paneWidth >= width {
		return uitheme.Paint(uitheme.Hrule(width), width, uitheme.BackdropHex())
	}
	fill := lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.PanelHex()))
	first := uitheme.Plain(fill.Render("🬹"), 1)
	interior := lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Render(strings.Repeat("🬂", paneWidth-1))
	last := lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Render("🬨")
	return first + uitheme.Paint(interior, paneWidth-1, uitheme.PanelHex()) + uitheme.Paint(last, 1, uitheme.PanelHex()) +
		uitheme.Paint(uitheme.Hrule(width-paneWidth-1), width-paneWidth-1, uitheme.BackdropHex())
}

// boundedRuleRow closes the body. Over the pane it draws half blocks in the
// pane's own tone — the fill bleeding half a cell past its box — and across
// the content it draws the thin rule, whose center line meets the half
// blocks' edge at the same height.
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
	if m.prefs.glyphMode.Effective() == presentation.GlyphModeBasic || paneWidth < 2 || paneWidth >= width {
		return uitheme.Paint(uitheme.Hrule(width), width, uitheme.BackdropHex())
	}
	facing, corner := "▄", "▜"
	if edge == "▄" {
		facing, corner = "▀", "▟"
	}
	first := uitheme.Plain(lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.PanelHex())).Render(facing), 1)
	interior := lipgloss.NewStyle().Foreground(uitheme.ColorBg()).
		Render(strings.Repeat(edge, paneWidth-1))
	last := lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Render(corner)
	return first + uitheme.Paint(interior, paneWidth-1, uitheme.PanelHex()) + uitheme.Paint(last, 1, uitheme.PanelHex()) +
		uitheme.Paint(uitheme.Hrule(width-paneWidth-1), width-paneWidth-1, uitheme.BackdropHex())
}

// railEdgeCell is one row of the pane's first column, drawn as a foreground
// block in the row's own tone rather than as cell background. The window
// margin beside it inherits the cell's background — the terminal's own —
// so the pane's left edge lands exactly on the cell grid.
func railEdgeCell(tone string) string {
	return uitheme.Plain(lipgloss.NewStyle().Foreground(lipgloss.Color(tone)).Render("█"), 1)
}

// vruleColumn is the seam between two painted columns. It doubles as the
// resize grip, taking the accent while the divider is being moved.
func (m *Model) vruleColumn(height int) []string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = m.seamCell(false)
	}
	return lines
}

// bleedColumn finishes a pane's right edge: a half block in the pane's
// tone on the backdrop, extending the fill half a cell past the seam.
func (m *Model) bleedColumn(height int) []string {
	background := uitheme.PanelHex()
	cell := uitheme.Paint(lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Render("▐"), 1, background)
	if m.prefs.glyphMode.Effective() == presentation.GlyphModeBasic {
		background = uitheme.BackdropHex()
		cell = uitheme.Paint(lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.RuleHex())).Render("│"), 1, background)
	}
	lines := make([]string, height)
	for i := range lines {
		lines[i] = cell
	}
	// Focus mode lights this column beside the pane rows alone, so the edge
	// traces the agent's terminal rather than the whole content panel. The
	// top corner sits on the rule row just above the capture.
	box := m.focus.pane.FrameBox()
	if m.focused() && box.Valid {
		edge := uitheme.Paint(uitheme.FocusEdgeStyle().Render("│"), 1, background)
		corner := uitheme.Paint(uitheme.FocusEdgeStyle().Render(m.chrome().TopLeftGlyph), 1, background)
		top := box.Y - m.listChromeRows()
		if top-1 >= 0 && top-1 < len(lines) {
			lines[top-1] = corner
		}
		for row := top; row < top+box.Height; row++ {
			if row >= 0 && row < len(lines) {
				lines[row] = edge
			}
		}
	}
	return lines
}

// focusBottomRule closes the focused pane's hairline along the frame rule
// under the body: the corner cell is the bleed column the left edge runs
// down, so the two meet exactly.
func (m *Model) focusBottomRule(paneWidth, width int) string {
	tail := width - paneWidth - 1
	if tail < 1 || paneWidth < 2 {
		return m.boundedRuleRow(paneWidth, width, "▄")
	}
	if m.prefs.glyphMode.Effective() == presentation.GlyphModeBasic {
		return uitheme.Paint(uitheme.Hrule(paneWidth), paneWidth, uitheme.BackdropHex()) +
			uitheme.Paint(uitheme.FocusEdgeStyle().Render(m.chrome().BottomLeftGlyph+strings.Repeat("─", tail)), tail+1, uitheme.BackdropHex())
	}
	first := uitheme.Plain(lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.PanelHex())).Render("▀"), 1)
	interior := lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Render(strings.Repeat("▄", paneWidth-1))
	corner := uitheme.FocusEdgeStyle().Render("╰")
	return first + uitheme.Paint(interior, paneWidth-1, uitheme.PanelHex()) + uitheme.Paint(corner, 1, uitheme.PanelHex()) +
		uitheme.Paint(uitheme.FocusEdgeStyle().Render(strings.Repeat("─", tail)), tail, uitheme.BackdropHex())
}

// seamCell is one row of the vertical seam. The column is the pane's own
// fill, so only the pane's rules cross it: a content rule carried across
// reads as the content's separator running into the sessions list.
func (m *Model) seamCell(railRule bool) string {
	// While the divider is being moved the seam becomes the grip.
	if m.layout.split.Dragging || m.layout.split.ResizeMode {
		color := uitheme.ColorAccent()
		if m.layout.split.Dragging {
			color = uitheme.ColorAccent2()
		}
		return uitheme.Paint(lipgloss.NewStyle().Foreground(color).Render(m.chrome().ResizeGlyph), 1, uitheme.PanelHex())
	}
	if railRule {
		return uitheme.Paint(uitheme.Hrule(1), 1, uitheme.PanelHex())
	}
	return uitheme.Paint("", 1, uitheme.PanelHex())
}

// contentLine is one row of the content column: ours to paint, a seam that
// spans the column edge to edge, or captured output that must stay on the
// backdrop. The Rail owns the type, since its rows also carry the tone the
// edge column beside them matches.
type contentLine = uirail.Line

// paintContent paints a content column, leaving raw rows unfilled.
func paintContent(lines []contentLine, width, height int, bg string) []string {
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i >= len(lines) {
			out[i] = uitheme.Paint("", width, bg)
			continue
		}
		if lines[i].Rule {
			// Seams are drawn at the column's full width, not the inset the
			// text uses, so they meet the frame's edges exactly.
			out[i] = uitheme.Paint(uitheme.Hrule(width), width, bg)
			continue
		}
		if lines[i].Raw {
			out[i] = uitheme.Plain(lines[i].Text, width)
			continue
		}
		out[i] = uitheme.Paint(lines[i].Text, width, bg)
	}
	return out
}
