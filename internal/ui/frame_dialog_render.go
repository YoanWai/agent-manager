package ui

import "strings"

type dialogRenderContext struct {
	width  int
	height int
	status string
}

// dialogBox is a card's own lines, before the backdrop surrounds them, and
// the keys its foot paints, from the card's own first row and column.
func dialogBox(ctx dialogRenderContext, width int, title, body string, hint [][2]string) ([]string, keyHits) {
	inner := cardInnerWidth(width)
	border := cardBorderStyle()
	pad := strings.Repeat(" ", cardPaddingX)
	edge := border.Render("│")

	row := func(line string) string {
		return paint(edge+pad+padRight(line, inner)+pad+edge, width, blockHex())
	}
	rule := func(left, right string) string {
		return paint(border.Render(left+strings.Repeat("─", width-2)+right), width, blockHex())
	}

	lines := []string{cardTitleRow(width, title, border), row("")}
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, row(line))
	}
	if ctx.status != "" {
		lines = append(lines, row(""), row(ctx.status))
	}
	lines = append(lines, row(""))
	var hits keyHits
	if len(hint) > 0 {
		lines = append(lines, rule("├", "┤"))
		foot := paintInline(hint, inner)
		hits = foot.hits.moved(cardBodyColumn, len(lines))
		for _, line := range strings.Split(foot.text, "\n") {
			lines = append(lines, row(line))
		}
	}
	lines = append(lines, rule("╰", "╯"))
	return lines, hits
}

// dialogPlacement is where centerDialog puts box: its first row, and the
// columns it spans.
func dialogPlacement(ctx dialogRenderContext, box []string) (top, left, right int) {
	width := maxLineWidth(box)
	height := max(ctx.height, len(box))
	left = max((ctx.width-width)/2, 0)
	top = max((height-len(box))/2, 0)
	return top, left, left + width
}

func centerDialog(ctx dialogRenderContext, box []string) string {
	width := maxLineWidth(box)
	height := max(ctx.height, len(box))
	top, left, _ := dialogPlacement(ctx, box)
	frameWidth := max(ctx.width, left+width)
	frame := make([]string, 0, height)
	for i := 0; i < height; i++ {
		row := ""
		if i >= top && i-top < len(box) {
			row = paint("", left, backdropHex()) + box[i-top]
		}
		frame = append(frame, paint(row, frameWidth, backdropHex()))
	}
	return strings.Join(frame, "\n")
}
