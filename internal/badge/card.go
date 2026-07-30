package badge

import (
	"fmt"
	"math"
	"strings"
)

// Stat is one column of the card: a figure people read first and the word
// that says what it counts.
type Stat struct {
	Value string
	Label string
	Color string
}

// Every run in the card is centred inside its own column, so the layout never
// depends on knowing a glyph's advance and the text is never stretched to fit
// a box that was measured wrong.
const (
	cardHeight   = 92
	cardPadX     = 8
	valueSize    = 30
	labelSize    = 11
	valueMid     = 46
	labelMid     = 68
	ruleInset    = 22
	minColumn    = 128
	cardFont     = "-apple-system,BlinkMacSystemFont,Segoe UI,Helvetica,Arial,sans-serif"
	labelSpacing = "0.09em"
)

// Card renders the stats as one composition: figures on a shared baseline,
// their labels under them, hairlines between the columns.
func Card(stats []Stat, p Palette) string {
	if len(stats) == 0 {
		return ""
	}
	column := float64(minColumn)
	for _, stat := range stats {
		if reach := math.Ceil((valueSize*0.62*float64(len([]rune(stat.Value)))+cardPadX*2)/4) * 4; reach > column {
			column = reach
		}
	}
	width := column * float64(len(stats))

	var body strings.Builder
	for i, stat := range stats {
		centre := column*float64(i) + column/2
		if i > 0 {
			x := column * float64(i)
			body.WriteString(fmt.Sprintf(
				`<line x1="%g" y1="%d" x2="%g" y2="%d" stroke="%s" stroke-width="1" opacity=".55"/>`,
				x, ruleInset, x, cardHeight-ruleInset, p.Border))
		}
		body.WriteString(fmt.Sprintf(
			`<text x="%g" y="%d" text-anchor="middle" fill="%s" font-family="%s" font-size="%d" font-weight="650" style="font-variant-numeric:tabular-nums">%s</text>`,
			centre, valueMid, stat.Color, cardFont, valueSize, escape(stat.Value)))
		body.WriteString(fmt.Sprintf(
			`<text x="%g" y="%d" text-anchor="middle" fill="%s" font-family="%s" font-size="%d" letter-spacing="%s">%s</text>`,
			centre, labelMid, p.Label, cardFont, labelSize, labelSpacing, escape(strings.ToUpper(stat.Label))))
	}

	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%d" viewBox="0 0 %g %d" role="img" aria-label="%s">%s</svg>`,
		width, cardHeight, width, cardHeight, escape(summary(stats)), body.String())
}

func summary(stats []Stat) string {
	parts := make([]string, 0, len(stats))
	for _, stat := range stats {
		parts = append(parts, stat.Value+" "+stat.Label)
	}
	return strings.Join(parts, ", ")
}
