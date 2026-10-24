package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/charmbracelet/lipgloss"
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
	return uiframe.CardChrome(width, hint, frameCap(), m.errBar.text != "")
}

// flexCardWidth picks a width that fits every content line, never under the
// default card width and never past the terminal edge.
func (m *Model) flexCardWidth(title, body string, hint [][2]string) int {
	status := ""
	if m.errBar.text != "" {
		status = m.statusMessage(m.glyphs().CardErrorGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
	}
	return uiframe.FlexCardWidth(m.layout.width, title, body, hint, status, frameCap())
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
	return uiframe.Centered(ctx, box, backdropHex(), paint)
}

func (m *Model) dialogRenderContext() uiframe.Context {
	status := ""
	if m.errBar.text != "" {
		status = m.statusMessage(m.glyphs().CardErrorGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
	}
	return uiframe.Context{Width: m.layout.width, Height: m.layout.height, Status: status}
}

func maxLineWidth(lines []string) int { return uiframe.MaxLineWidth(lines) }
