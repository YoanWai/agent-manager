package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) cardWidth() int {
	width := 64
	if m.layout.width >= 28 && width > m.layout.width-4 {
		width = m.layout.width - 4
	}
	return width
}

const (
	cardPaddingX = 3
	// cardChromeX is the two border columns a card spends on its frame.
	cardChromeX = 2
)

// cardBorderStyle is the card's frame: the theme's border tone pulled toward
// the accent, so a dialog reads as the app's own surface rather than as a
// box drawn around it.
func cardBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current().Border, current().Accent, 0.35)))
}

// card floats a modal on the app backdrop: a framed panel with its title set
// into the top edge and its keys on a foot below a hairline.
func (m *Model) card(title, body string, hint [][2]string) string {
	return m.cardSized(m.cardWidth(), title, body, hint)
}

// cardFlex is card, but the panel grows with its content up to the terminal
// width so long settings rows are not clipped.
func (m *Model) cardFlex(title, body string, hint [][2]string) string {
	return m.cardSized(m.flexCardWidth(title, body, hint), title, body, hint)
}

func cardInnerWidth(width int) int { return width - cardChromeX - 2*cardPaddingX }

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
	rows := 5 + strings.Count(legendInline(hint, cardInnerWidth(width)), "\n") + 1
	if m.errBar.text != "" {
		rows += 2
	}
	return rows
}

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

// cardSized is card at an explicit width, for the key map, whose lines are
// too long to read inside the default column.
func (m *Model) cardSized(width int, title, body string, hint [][2]string) string {
	box, hits := dialogBox(m.dialogRenderContext(), width, title, body, hint)
	frame := m.centerOnBackdrop(box)
	card := &m.layout.card
	card.bodyTop, card.bodyLeft, card.bodyWidth = card.top+cardBodyRow, card.left+cardBodyColumn, cardInnerWidth(width)
	m.placeKeys(hits, card.left, card.top)
	return frame
}

// cardBodyRow and cardBodyColumn place a card's body inside it: under the
// title edge and the blank row after it, past the left edge and the padding.
const (
	cardBodyRow    = 2
	cardBodyColumn = 1 + cardPaddingX
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

// cardTitleRow sets the title into the top edge, so the frame names the
// dialog instead of spending a content row on it.
func cardTitleRow(width int, title string, border lipgloss.Style) string {
	label := " " + cardTitle(title) + " "
	dashes := width - 4 - lipgloss.Width(label)
	if dashes < 0 {
		dashes = 0
	}
	head := border.Render("╭──") + label + border.Render(strings.Repeat("─", dashes)+"╮")
	return paint(head, width, blockHex())
}

// centerOnBackdrop floats a block of pre-painted lines in the middle of
// the app frame, filling the rest with the backdrop. The backdrop covers
// any key painted before it, so those keys stop answering clicks.
func (m *Model) centerOnBackdrop(box []string) string {
	ctx := m.dialogRenderContext()
	top, left, right := dialogPlacement(ctx, box)
	m.layout.card = cardRect{top: top, left: left, right: right, bottom: top + len(box)}
	m.layout.keyHits = nil
	return centerDialog(ctx, box)
}

func maxLineWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width
}

func (m *Model) dialogRenderContext() dialogRenderContext {
	status := ""
	if m.errBar.text != "" {
		status = m.statusMessage(m.glyphs().CardErrorGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
	}
	return dialogRenderContext{width: m.layout.width, height: m.layout.height, status: status}
}
