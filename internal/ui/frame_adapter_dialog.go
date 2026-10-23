package ui

import (
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/charmbracelet/lipgloss"
)

const (
	cardPaddingX = uiframe.CardPaddingX
	// cardChromeX is the two border columns a card spends on its frame.
	cardChromeX = uiframe.CardChromeX
)

// cardBodyRow and cardBodyColumn place a card's body inside it: under the
// title edge and the blank row after it, past the left edge and the padding.
const (
	cardBodyRow    = uiframe.CardBodyRow
	cardBodyColumn = uiframe.CardBodyColumn
)

// cardRect is a painted card's cells, and its body's: the first body line,
// the column it starts at and how many columns it spans.
type cardRect struct {
	top, left, right, bottom     int
	bodyTop, bodyLeft, bodyWidth int
}

func (r cardRect) holds(x, y int) bool {
	return x >= r.left && x < r.right && y >= r.top && y < r.bottom
}

// bodyAt is the body line and column a point lands on, when it lands on
// the body's columns.
func (r cardRect) bodyAt(x, y int) (col, line int, ok bool) {
	col, line = x-r.bodyLeft, y-r.bodyTop
	return col, line, r.holds(x, y) && col >= 0 && col < r.bodyWidth && line >= 0
}

func (m *Model) cardWidth() int { return cardWidthFor(m.layout.width) }

func cardWidthFor(cols int) int { return uiframe.CardWidthFor(cols) }

// cardBorderStyle is the card's frame: the theme's border tone pulled toward
// the accent, so a dialog reads as the app's own surface rather than as a
// box drawn around it.
func cardBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current().Border, current().Accent, 0.35)))
}

// card floats a modal on the app backdrop: a framed panel with its title set
// into the top edge and its keys on a foot below a hairline.
func (m *Model) card(title, body string, hint [][2]string) string {
	return m.CardSized(m.cardWidth(), title, body, hint)
}

// cardFlex is card, but the panel grows with its content up to the terminal
// width so long settings rows are not clipped.
func (m *Model) cardFlex(title, body string, hint [][2]string) string {
	return m.CardSized(m.flexCardWidth(title, body, hint), title, body, hint)
}

func cardInnerWidth(width int) int { return uiframe.CardInnerWidth(width) }

// cardChromeRows are the rows a default-width card spends around its body:
// the title edge and the blank under it, the status, the blank above the
// hint, the hint's rule and rows, and the bottom edge.
func (m *Model) cardChromeRows(hint [][2]string) int {
	return m.chromeRowsAt(m.cardWidth(), hint)
}

// flexChromeRows are the rows a flex card spends around its body. A flex
// card is at least as wide as its hint on one row, so the hint wraps the
// way it would at that width whatever the body holds.
func (m *Model) flexChromeRows(hint [][2]string) int {
	return m.chromeRowsAt(m.flexCardWidth("", "", hint), hint)
}

func (m *Model) chromeRowsAt(width int, hint [][2]string) int {
	// The hint's rule and rows, one row per line it wraps to.
	return uiframe.ChromeRows(textRows(legendInline(hint, cardInnerWidth(width))), m.errBar.text != "")
}

// textRows is the line count of a block of painted text.
func textRows(text string) int { return strings.Count(text, "\n") + 1 }

// flexCardWidth picks a width that fits every content line, never under the
// default card width and never past the terminal edge.
func (m *Model) flexCardWidth(title, body string, hint [][2]string) int {
	need := m.cardWidth()
	measure := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			if w := lipgloss.Width(line) + cardChromeX + 2*cardPaddingX; w > need {
				need = w
			}
		}
	}
	measure(body)
	measure(cardTitle(title))
	measure(legendInline(hint, 1<<30))
	if m.errBar.text != "" {
		measure(m.statusMessage(m.glyphs().CardErrorGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph))
	}
	if m.layout.width >= 28 && need > m.layout.width-4 {
		need = m.layout.width - 4
	}
	return need
}

func cardTitle(title string) string {
	return lipgloss.NewStyle().Foreground(colorAccent()).Bold(true).Render(title)
}

// CardSized is card at an explicit width, for the key map, whose lines are
// too long to read inside the default column.
func (m *Model) CardSized(width int, title, body string, hint [][2]string) string {
	box, hits := uiframe.Box(m.dialogRenderContext(), m.cardChrome(), width, title, body, hint, paint)
	frame := m.centerOnBackdrop(box)
	card := &m.layout.card
	card.bodyTop, card.bodyLeft, card.bodyWidth = card.top+cardBodyRow, card.left+cardBodyColumn, cardInnerWidth(width)
	m.placeKeys(rootHits(hits), card.left, card.top)
	return frame
}

// cardChrome is the card's live painting: its edge tone, the accent its title
// reads in, the block fill inside it, and the caps on its foot.
func (m *Model) cardChrome() uiframe.Chrome {
	return uiframe.Chrome{
		Border: cardBorderStyle(),
		Title:  lipgloss.NewStyle().Foreground(colorAccent()).Bold(true),
		Block:  blockHex(),
		Cap:    frameCap(),
	}
}

// centerOnBackdrop floats a block of pre-painted lines in the middle of
// the app frame, filling the rest with the backdrop. The backdrop covers
// any key painted before it, so those keys stop answering clicks.
func (m *Model) centerOnBackdrop(box []string) string {
	ctx := m.dialogRenderContext()
	top, left, right := uiframe.Placement(ctx, box)
	m.layout.card = cardRect{top: top, left: left, right: right, bottom: top + len(box)}
	m.layout.keyHits = nil
	return centerDialog(ctx, box)
}

// centerDialog floats box in the middle of the frame, filling the rest with
// the backdrop.
func centerDialog(ctx uiframe.Context, box []string) string {
	width := uiframe.MaxLineWidth(box)
	height := max(ctx.Height, len(box))
	top, left, _ := uiframe.Placement(ctx, box)
	frameWidth := max(ctx.Width, left+width)
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

func (m *Model) dialogRenderContext() uiframe.Context {
	status := ""
	if m.errBar.text != "" {
		status = m.statusMessage(m.glyphs().CardErrorGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
	}
	return uiframe.Context{Width: m.layout.width, Height: m.layout.height, Status: status}
}

func maxLineWidth(lines []string) int { return uiframe.MaxLineWidth(lines) }
