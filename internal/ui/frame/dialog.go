package frame

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// CardPaddingX is the blank columns a card keeps inside its edge.
	CardPaddingX = 3
	// CardChromeX is the two border columns a card spends on its frame.
	CardChromeX = 2
	// These place a card's body inside it: under the title edge and the blank
	// row after it, past the left edge and the padding.
	CardBodyRow    = 2
	CardBodyColumn = 1 + CardPaddingX
)

// Fill pads a pre-painted line to width and fills its cells with bg.
type Fill func(s string, width int, bg string) string

// CardWidthFor is the default card width on a terminal cols wide.
func CardWidthFor(cols int) int {
	width := 64
	if cols >= 28 && width > cols-4 {
		width = cols - 4
	}
	return width
}

func CardInnerWidth(width int) int { return width - CardChromeX - 2*CardPaddingX }

// ChromeRows is the rows a card spends around its body: the title edge and
// the blank under it, the blank above the hint, the hint's rule and its
// hintRows rows, and the bottom edge; status adds the status block above the
// hint.
func ChromeRows(hintRows int, status bool) int {
	rows := 5 + hintRows
	if status {
		rows += 2
	}
	return rows
}

// Context is the terminal a card floats over. Status is the status line the
// frame would show if it had one.
type Context struct {
	Width  int
	Height int
	Status string
}

// Chrome is what painting a card needs from the live theme: its edge tone,
// the accent its title reads in, the block fill inside it, and the caps on
// its foot.
type Chrome struct {
	Border lipgloss.Style
	Title  lipgloss.Style
	Block  string
	Cap    Cap
}

// Box is a card's own lines, before the backdrop surrounds them, and the keys
// its foot paints, from the card's own first row and column.
func Box(ctx Context, chrome Chrome, width int, title, body string, hint [][2]string, fill Fill) ([]string, KeyHits) {
	inner := CardInnerWidth(width)
	pad := strings.Repeat(" ", CardPaddingX)
	edge := chrome.Border.Render("│")

	row := func(line string) string {
		return fill(edge+pad+presentation.PadRight(line, inner)+pad+edge, width, chrome.Block)
	}
	rule := func(left, right string) string {
		return fill(chrome.Border.Render(left+strings.Repeat("─", width-2)+right), width, chrome.Block)
	}

	lines := []string{titleRow(width, title, chrome, fill), row("")}
	if body != "" {
		for _, line := range strings.Split(body, "\n") {
			lines = append(lines, row(line))
		}
	}
	if ctx.Status != "" {
		lines = append(lines, row(""), row(ctx.Status))
	}
	if body != "" {
		lines = append(lines, row(""))
	}
	var hits KeyHits
	if len(hint) > 0 {
		lines = append(lines, rule("├", "┤"))
		foot := PaintInline(hint, inner, chrome.Cap)
		hits = foot.Hits.Moved(CardBodyColumn, len(lines))
		for _, line := range strings.Split(foot.Text, "\n") {
			lines = append(lines, row(line))
		}
	}
	lines = append(lines, rule("╰", "╯"))
	return lines, hits
}

// titleRow sets the title into the top edge, so the frame names the dialog
// instead of spending a content row on it.
func titleRow(width int, title string, chrome Chrome, fill Fill) string {
	label := " " + chrome.Title.Render(title) + " "
	dashes := width - 4 - lipgloss.Width(label)
	if dashes < 0 {
		dashes = 0
	}
	head := chrome.Border.Render("╭──") + label + chrome.Border.Render(strings.Repeat("─", dashes)+"╮")
	return fill(head, width, chrome.Block)
}

// Placement is where the frame centers a box: its first row, and the columns
// it spans.
func Placement(ctx Context, box []string) (top, left, right int) {
	width := MaxLineWidth(box)
	height := max(ctx.Height, len(box))
	left = max((ctx.Width-width)/2, 0)
	top = max((height-len(box))/2, 0)
	return top, left, left + width
}

func MaxLineWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			width = w
		}
	}
	return width
}
