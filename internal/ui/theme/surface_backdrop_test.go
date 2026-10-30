package theme

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

// cellColors lays a frame out the way a terminal would and reports each
// cell's background and foreground, nil where the terminal's own color shows.
func cellColors(frame string, width, height int) (backgrounds, foregrounds [][]color.Color) {
	buffer := cellbuf.NewBuffer(width, height)
	cellbuf.SetContent(buffer, frame)
	backgrounds = make([][]color.Color, height)
	foregrounds = make([][]color.Color, height)
	for y := range height {
		backgrounds[y] = make([]color.Color, width)
		foregrounds[y] = make([]color.Color, width)
		for x := range width {
			cell := buffer.Cell(x, y)
			if cell.Width == 0 && x > 0 {
				// The trailing half of a wide character wears its lead's colors.
				backgrounds[y][x], foregrounds[y][x] = backgrounds[y][x-1], foregrounds[y][x-1]
				continue
			}
			backgrounds[y][x], foregrounds[y][x] = cell.Style.Bg, cell.Style.Fg
		}
	}
	return backgrounds, foregrounds
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

func hexColor(hex string) color.Color {
	r, g, b := HexRGB(hex)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
}

func useTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// Every cell the frame left on the terminal's colors takes the theme's,
// whichever way the reset was spelled, and every cell that carried a color
// of its own keeps it.
func TestFillBackdropPaintsOnlyDefaultCells(t *testing.T) {
	useTrueColor(t)
	const width = 24
	chip := "\x1b[48;2;1;2;3m"
	frame := strings.Join([]string{
		"plain text",
		lipgloss.NewStyle().Foreground(lipgloss.Color("#123456")).Render("styled") + " tail",
		chip + "chip\x1b[49m after",
		chip + "chip\x1b[m after",
		chip + "chip\x1b[0;1;38;2;0;0;0m black on default\x1b[0m",
		chip + "a\x1b[38;5;0mb\x1b[38;2;0;0;0mc stays on chip\x1b[0m",
		chip + "A\x1b[4:0mB\x1b[4:3mC stays on chip\x1b[0m",
		"\x1b[48:2::9:8:7mcolon\x1b[38:2::0:0:0m fg only\x1b[0m",
		"\x1b[44mindexed\x1b[0m wide 界 cell",
		"\x1b[31mred\x1b[39m default ink \x1b[1;91mbright\x1b[22;39m ink",
		"",
	}, "\n")
	height := strings.Count(frame, "\n") + 1

	beforeBg, beforeFg := cellColors(frame, width, height)
	afterBg, afterFg := cellColors(FillBackdrop(frame, width, "#f7f7f5", "#33333a"), width, height)
	for y := range height {
		for x := range width {
			if want := orTheme(beforeBg[y][x], "#f7f7f5"); !sameColor(afterBg[y][x], want) {
				t.Errorf("row %d col %d background = %v, want %v", y, x, afterBg[y][x], want)
			}
			if want := orTheme(beforeFg[y][x], "#33333a"); !sameColor(afterFg[y][x], want) {
				t.Errorf("row %d col %d foreground = %v, want %v", y, x, afterFg[y][x], want)
			}
		}
	}
}

func orTheme(own color.Color, fallback string) color.Color {
	if own == nil {
		return hexColor(fallback)
	}
	return own
}

func TestFillBackdropKeepsText(t *testing.T) {
	useTrueColor(t)
	frame := "one \x1b[1mtwo\x1b[m\n\x1b[38;5;42mthree\x1b[39m"
	got := strings.Split(ansi.Strip(FillBackdrop(frame, 8, "#f7f7f5", "#33333a")), "\n")
	want := []string{"one two ", "three   "}
	if !slices.Equal(got, want) {
		t.Fatalf("filled text = %q, want %q", got, want)
	}
}
