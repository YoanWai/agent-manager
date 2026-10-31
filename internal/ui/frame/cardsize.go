package frame

import "github.com/YoanWai/agent-manager/internal/ui/presentation"

// CardSizing is one card's geometry: the terminal it floats in, the status
// block it paints and the hint tier on its foot.
type CardSizing struct {
	Mode   presentation.GlyphMode
	Cols   int
	Status string
	Hint   [][2]string
}

func (c CardSizing) Width() int { return CardWidthFor(c.Cols) }

func (c CardSizing) ChromeRows(width int) int {
	return CardChrome(width, c.Hint, LiveCap(c.Mode), c.Status != "")
}

func (c CardSizing) DefaultChromeRows() int { return c.ChromeRows(c.Width()) }

// FlexChromeRows are the rows a flex card spends around its body. A flex card
// is at least as wide as its hint on one row, so the hint wraps the way it
// would at that width whatever the body holds.
func (c CardSizing) FlexChromeRows() int { return c.ChromeRows(c.FlexWidth("", "")) }

// FlexWidth picks a width that fits every content line, never under the
// default card width and never past the terminal edge.
func (c CardSizing) FlexWidth(title, body string) int {
	return FlexCardWidth(c.Cols, title, body, c.Hint, c.Status, LiveCap(c.Mode))
}
