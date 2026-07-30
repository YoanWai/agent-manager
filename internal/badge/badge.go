// Package badge renders the README's status chips as SVG the repo owns,
// so their type, palette and spacing match the TUI instead of a badge
// host's house style.
package badge

import (
	"fmt"
	"strings"
)

// Palette is one rendering of a chip: the surface it sits on, its border,
// and the label tone. Light and dark variants pair with the reader's GitHub
// theme through a <picture> element.
type Palette struct {
	Surface string
	Border  string
	Label   string
}

var (
	Light = Palette{Surface: "#ffffff", Border: "#d5dbe2", Label: "#59636e"}
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

// The chips are set in the monospace face a terminal tool should wear, which
// also makes their width exact: every glyph advances the same 0.6em, so the
// box is measured rather than guessed and the text never has to be stretched
// to fit it.
const (
	height   = 26
	radius   = 5
	fontSize = 12
	advance  = fontSize * 0.6
	padX     = 11
	gap      = 7
	iconSize = 12
)

const fontStack = "ui-monospace,SFMono-Regular,SF Mono,Menlo,Consolas,Liberation Mono,monospace"

// icons are 16x16 Octicon paths, scaled to iconSize at draw time. Each is a
// single filled path so it inherits the chip's label tone without extra markup.
var icons = map[string]string{
	"star": "M8 .25a.75.75 0 0 1 .673.418l1.882 3.815 4.21.612a.75.75 0 0 1 .416 1.279l-3.046 2.97.719 4.192a.75.75 0 0 1-1.088.791L8 12.347l-3.766 1.98a.75.75 0 0 1-1.088-.79l.72-4.194L.818 6.374a.75.75 0 0 1 .416-1.28l4.21-.611L7.327.668A.75.75 0 0 1 8 .25Z",
	"tag":  "M1 7.775V2.75C1 1.784 1.784 1 2.75 1h5.025c.464 0 .91.184 1.238.513l6.25 6.25a1.75 1.75 0 0 1 0 2.474l-5.026 5.026a1.75 1.75 0 0 1-2.474 0l-6.25-6.25A1.752 1.752 0 0 1 1 7.775ZM4.5 5a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z",
	"repo": "M2 2.5A2.5 2.5 0 0 1 4.5 0h8.75a.75.75 0 0 1 .75.75v12.5a.75.75 0 0 1-.75.75h-2.5a.75.75 0 0 1 0-1.5h1.75v-2h-8a1 1 0 0 0-.714 1.7.75.75 0 1 1-1.072 1.05A2.495 2.495 0 0 1 2 11.5Zm10.5-1h-8a1 1 0 0 0-1 1v6.708A2.486 2.486 0 0 1 4.5 9h8Z",
	"law":  "M8 1a.75.75 0 0 1 .75.75v.75h3.5a.75.75 0 0 1 0 1.5h-.51l2.22 5.03a.75.75 0 0 1-.2.87A3.6 3.6 0 0 1 11.5 11a3.6 3.6 0 0 1-2.26-1.1.75.75 0 0 1-.2-.87L11.26 4H8.75v8.5h2a.75.75 0 0 1 0 1.5h-5.5a.75.75 0 0 1 0-1.5h2V4H4.74l2.22 5.03a.75.75 0 0 1-.2.87A3.6 3.6 0 0 1 4.5 11a3.6 3.6 0 0 1-2.26-1.1.75.75 0 0 1-.2-.87L4.26 4h-.51a.75.75 0 0 1 0-1.5h3.5v-.75A.75.75 0 0 1 8 1Z",
}

func span(text string) float64 { return advance * float64(len([]rune(text))) }

// Width is the chip's rendered width, summed from the same advances the text
// is drawn with.
func (c Chip) Width() float64 {
	width := padX*2 + span(c.Label) + gap + span(c.Value)
	if _, ok := icons[c.Icon]; ok {
		width += iconSize + gap
	}
	return width
}

// SVG renders the chip as a standalone document sized to its content.
func (c Chip) SVG(p Palette) string {
	width := c.Width()
	cursor := float64(padX)
	var body strings.Builder
	if path, ok := icons[c.Icon]; ok {
		body.WriteString(fmt.Sprintf(
			`<g transform="translate(%g %g) scale(%g)"><path fill="%s" d="%s"/></g>`,
			cursor, float64(height-iconSize)/2, float64(iconSize)/16, p.Label, path))
		cursor += iconSize + gap
	}
	baseline := float64(height)/2 + fontSize*0.35
	body.WriteString(run(cursor, baseline, span(c.Label), p.Label, "400", c.Label))
	cursor += span(c.Label) + gap
	body.WriteString(run(cursor, baseline, span(c.Value), c.Color, "700", c.Value))

	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%d" viewBox="0 0 %g %d" role="img" aria-label="%s: %s">`+
			`<rect x=".5" y=".5" width="%g" height="%d" rx="%d" fill="%s" stroke="%s"/>%s</svg>`,
		width, height, width, height, escape(c.Label), escape(c.Value),
		width-1, height-1, radius, p.Surface, p.Border, body.String())
}

// run pins one text run to the width the box reserved for it. lengthAdjust
// stays on "spacing" so a face whose advance differs from the assumed 0.6em
// shifts the tracking instead of distorting the letterforms.
func run(x, baseline, length float64, fill, weight, content string) string {
	return fmt.Sprintf(
		`<text x="%g" y="%g" textLength="%g" lengthAdjust="spacing" fill="%s" font-family="%s" font-size="%d" font-weight="%s" xml:space="preserve">%s</text>`,
		x, baseline, length, fill, fontStack, fontSize, weight, escape(content))
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
