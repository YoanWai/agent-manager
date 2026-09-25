package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) cardWidth() int {
	width := 64
	if m.width >= 28 && width > m.width-4 {
		width = m.width - 4
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
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Border, current.Accent, 0.35)))
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
		measure(m.statusMessage("⚠", "●", "▲"))
	}
	if m.width >= 28 && need > m.width-4 {
		need = m.width - 4
	}
	return need
}

func cardTitle(title string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
}

// cardSized is card at an explicit width, for the key map, whose lines are
// too long to read inside the default column.
func (m *Model) cardSized(width int, title, body string, hint [][2]string) string {
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
	if m.errBar.text != "" {
		lines = append(lines, row(""), row(m.statusMessage("⚠", "●", "▲")))
	}
	lines = append(lines, row(""))
	if len(hint) > 0 {
		lines = append(lines, rule("├", "┤"))
		for _, line := range strings.Split(legendInline(hint, inner), "\n") {
			lines = append(lines, row(line))
		}
	}
	lines = append(lines, rule("╰", "╯"))
	return m.centerOnBackdrop(lines)
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
// the app frame, filling the rest with the backdrop.
func (m *Model) centerOnBackdrop(box []string) string {
	width := maxLineWidth(box)
	height := max(m.height, len(box))
	left := max((m.width-width)/2, 0)
	frameWidth := max(m.width, left+width)
	top := max((height-len(box))/2, 0)
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

func maxLineWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width
}
