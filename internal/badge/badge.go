// Package badge renders the README's status chips as SVG the repo owns,
// so their type, palette and spacing match the TUI instead of a badge
// host's house style.
package badge

import (
	"fmt"
	"strings"
)

// Palette is one rendering of a chip: the surface it sits on, its border,
// and the two text tones. Light and dark variants pair with the reader's
// GitHub theme through a <picture> element.
type Palette struct {
	Surface string
	Border  string
	Label   string
}

var (
	Light = Palette{Surface: "#ffffff", Border: "#d8dee4", Label: "#59636e"}
	Dark  = Palette{Surface: "#151921", Border: "#2d333d", Label: "#98a0ac"}
)

// Chip is one rendered pill: a muted label, a coloured value, and an
// optional glyph that opens the pill.
type Chip struct {
	Label string
	Value string
	Color string
	Icon  string
}

const (
	height     = 28
	radius     = 6
	fontSize   = 12
	padX       = 10
	gapIcon    = 6
	gapLabel   = 6
	iconSize   = 12
	labelWidth = 6.35
	valueWidth = 6.85
)

const fontStack = "-apple-system,BlinkMacSystemFont,Segoe UI,Helvetica,Arial,sans-serif"

// icons are 12x12 paths on a 16x16 viewBox, scaled at draw time. Each is a
// single filled path so it inherits the chip's tone without extra markup.
var icons = map[string]string{
	"star":     "M8 .25a.75.75 0 0 1 .673.418l1.882 3.815 4.21.612a.75.75 0 0 1 .416 1.279l-3.046 2.97.719 4.192a.75.75 0 0 1-1.088.791L8 12.347l-3.766 1.98a.75.75 0 0 1-1.088-.79l.72-4.194L.818 6.374a.75.75 0 0 1 .416-1.28l4.21-.611L7.327.668A.75.75 0 0 1 8 .25Z",
	"download": "M7.25 1.75a.75.75 0 0 1 1.5 0v7.19l2.22-2.22a.75.75 0 1 1 1.06 1.06l-3.5 3.5a.75.75 0 0 1-1.06 0l-3.5-3.5a.75.75 0 0 1 1.06-1.06l2.22 2.22V1.75ZM2.5 12.5a.75.75 0 0 1 .75.75v.5h9.5v-.5a.75.75 0 0 1 1.5 0v1.25a.75.75 0 0 1-.75.75H2.5a.75.75 0 0 1-.75-.75V13.25a.75.75 0 0 1 .75-.75Z",
	"tag":      "M1 7.775V2.75C1 1.784 1.784 1 2.75 1h5.025c.464 0 .91.184 1.238.513l6.25 6.25a1.75 1.75 0 0 1 0 2.474l-5.026 5.026a1.75 1.75 0 0 1-2.474 0l-6.25-6.25A1.752 1.752 0 0 1 1 7.775ZM4.5 5a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z",
	"repo":     "M2 2.5A2.5 2.5 0 0 1 4.5 0h8.75a.75.75 0 0 1 .75.75v12.5a.75.75 0 0 1-.75.75h-2.5a.75.75 0 0 1 0-1.5h1.75v-2h-8a1 1 0 0 0-.714 1.7.75.75 0 1 1-1.072 1.05A2.495 2.495 0 0 1 2 11.5Zm10.5-1h-8a1 1 0 0 0-1 1v6.708A2.486 2.486 0 0 1 4.5 9h8Z",
}

// Width is the chip's rendered width. Text is drawn with textLength, so the
// estimate here is also the exact width the glyphs are fitted into.
func (c Chip) Width() float64 {
	width := float64(padX)*2 + c.labelSpan() + gapLabel + c.valueSpan()
	if c.Icon != "" {
		width += iconSize + gapIcon
	}
	return width
}

func (c Chip) labelSpan() float64 { return labelWidth * float64(len([]rune(c.Label))) }
func (c Chip) valueSpan() float64 { return valueWidth * float64(len([]rune(c.Value))) }

// SVG renders the chip as a standalone document sized to its content.
func (c Chip) SVG(p Palette) string {
	width := c.Width()
	cursor := float64(padX)
	var body strings.Builder
	if path, ok := icons[c.Icon]; ok {
		scale := float64(iconSize) / 16
		body.WriteString(fmt.Sprintf(
			`<g transform="translate(%.1f %.1f) scale(%.4f)"><path fill="%s" d="%s"/></g>`,
			cursor, float64(height-iconSize)/2, scale, p.Label, path))
		cursor += iconSize + gapIcon
	}
	baseline := height/2 + fontSize*0.36
	body.WriteString(fmt.Sprintf(
		`<text x="%.1f" y="%.1f" textLength="%.1f" lengthAdjust="spacingAndGlyphs" fill="%s" font-family="%s" font-size="%d">%s</text>`,
		cursor, baseline, c.labelSpan(), p.Label, fontStack, fontSize, escape(c.Label)))
	cursor += c.labelSpan() + gapLabel
	body.WriteString(fmt.Sprintf(
		`<text x="%.1f" y="%.1f" textLength="%.1f" lengthAdjust="spacingAndGlyphs" fill="%s" font-family="%s" font-size="%d" font-weight="600">%s</text>`,
		cursor, baseline, c.valueSpan(), c.Color, fontStack, fontSize, escape(c.Value)))

	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%d" viewBox="0 0 %.0f %d" role="img" aria-label="%s: %s">`+
			`<rect x=".5" y=".5" width="%.0f" height="%d" rx="%d" fill="%s" stroke="%s"/>%s</svg>`,
		width, height, width, height, escape(c.Label), escape(c.Value),
		width-1, height-1, radius, p.Surface, p.Border, body.String())
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
