package frame

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// CardChrome is the rows a card spends around its body at width, for a hint
// painted as one tier: the title edge and the blank under it, the blank
// above the hint, the hint's rule and its own rows, and the bottom edge;
// status adds the status block above the hint.
func CardChrome(width int, hint [][2]string, cap Cap, status bool) int {
	return ChromeRows(strings.Count(LegendInline(hint, CardInnerWidth(width), cap), "\n")+1, status)
}

// FlexCardWidth picks a card width that fits every content line, never under
// the default card width and never past the terminal edge. status is the
// status block the card would paint, empty when it paints none.
func FlexCardWidth(cols int, title, body string, hint [][2]string, status string, cap Cap) int {
	need := CardWidthFor(cols)
	measure := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			if w := lipgloss.Width(line) + CardChromeX + 2*CardPaddingX; w > need {
				need = w
			}
		}
	}
	measure(body)
	measure(title)
	measure(LegendInline(hint, 1<<30, cap))
	if status != "" {
		measure(status)
	}
	if cols >= 28 && need > cols-4 {
		need = cols - 4
	}
	return need
}

// Centered floats a block of pre-painted lines in the middle of a frame
// width wide and height tall, filling the rest with backdrop. fill paints a
// run of cells against the backdrop.
func Centered(ctx Context, box []string, backdrop string, fill Fill) string {
	width := MaxLineWidth(box)
	height := max(ctx.Height, len(box))
	top, left, _ := Placement(ctx, box)
	frameWidth := max(ctx.Width, left+width)
	frame := make([]string, 0, height)
	for i := 0; i < height; i++ {
		row := ""
		if i >= top && i-top < len(box) {
			row = fill("", left, backdrop) + box[i-top]
		}
		frame = append(frame, fill(row, frameWidth, backdrop))
	}
	return strings.Join(frame, "\n")
}

// HeaderRow is the header band: left against the left edge and the richest
// reading that fits set against the right edge, with gutter columns kept past
// that edge. An empty reading, or a band too narrow to hold both, leaves left
// alone.
func HeaderRow(left string, readings []string, width, gutter int) string {
	for _, right := range readings {
		gap := width - gutter - ansi.StringWidth(left) - ansi.StringWidth(right)
		if right == "" || gap < 2 {
			continue
		}
		return left + strings.Repeat(" ", gap) + right + strings.Repeat(" ", gutter)
	}
	return left
}
