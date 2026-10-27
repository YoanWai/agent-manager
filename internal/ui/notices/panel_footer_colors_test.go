package notices

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

// These colors have exact 256-color equivalents, so the role expectations
// below are independent of the renderer's profile conversion.
var (
	footBodyColor   = color.RGBA{R: 95, G: 95, A: 255}
	footMutedColor  = color.RGBA{R: 175, G: 175, B: 175, A: 255}
	footKeyColor    = color.RGBA{G: 255, B: 175, A: 255}
	footBorderColor = color.RGBA{R: 215, G: 175, A: 255}
)

func footerColorTheme() Theme {
	t := testTheme()
	t.Muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#afafaf"))
	t.Key = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ffaf"))
	t.Border = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7af00"))
	// Match noticesTheme's explicit body paint, including resets inside rows.
	t.Fill = func(row string, width int) string {
		return presentation.Fill(presentation.PadRight(row, width), "#5f5f00")
	}
	return t
}

func TestNoticeFooterBackgroundRoles(t *testing.T) {
	previous := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	for _, profile := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256} {
		lipgloss.SetColorProfile(profile)
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			enter := "↵"
			if mode == presentation.GlyphModeBasic {
				enter = "enter"
			}
			for _, tc := range []struct {
				width, inner int
				rows         []string
			}{
				{80, 62, []string{"r refresh · u update · " + enter + " open · x dismiss · esc/q"}},
				{30, 22, []string{"r refresh · u update", enter + " open · x dismiss", "esc/q"}},
				{20, 12, []string{"r refresh", "u update", enter + " open", "x dismiss", "esc/q"}},
				{15, 7, footerBareRows(mode, 15)},
				{12, 4, footerBareRows(mode, 12)},
				{10, 2, footerBareRows(mode, 10)},
			} {
				for _, height := range []int{40, 8} {
					t.Run(fmt.Sprintf("%d/%s/%d/%d", profile, mode, tc.width, height), func(t *testing.T) {
						p := NewPanel(nil)
						f := Frame{Width: tc.width, Height: height, GlyphMode: mode, Theme: footerColorTheme()}
						notices := footNotices("update-test")
						view := p.View(notices, f)
						first := len(view) - 1 - len(tc.rows)
						if tc.width == 80 {
							first = len(view) - 1
						}
						buffer := footerColorBuffer(view, tc.inner+4)
						var wantKeys []presentation.PaintedKey
						labels := []string{"r", "u", "↵", "x", "esc/q"}
						for i, text := range tc.rows {
							y := first + i
							line := ansi.Strip(view[y])
							if got := ansi.Cut(line, 2, 2+ansi.StringWidth(text)); got != text {
								t.Fatalf("footer row %d = %q, want %q", i, got, text)
							}
							assertFooterColorRow(t, buffer, y, tc.inner+4)
							x := 2
							for _, part := range strings.Split(text, " · ") {
								label := labels[len(wantKeys)]
								ink := footMutedColor
								if label == "u" {
									ink = footKeyColor
								}
								end := x + ansi.StringWidth(part)
								for col := x; col < end; col++ {
									if got := buffer.Cell(col, y).Style.Fg; !footerSameColor(got, ink) {
										t.Errorf("footer ink at (%d,%d) = %v, want %v", col, y, got, ink)
									}
								}
								wantKeys = append(wantKeys, presentation.PaintedKey{X0: x, X1: end, Y: y - (len(view) - 1), Label: label})
								x = end + 3
							}
							for col, r := range []rune(text) {
								if r == '·' {
									for x := col + 1; x <= col+3; x++ {
										if got := buffer.Cell(x, y).Style.Fg; !footerSameColor(got, footMutedColor) {
											t.Errorf("footer separator/space ink at (%d,%d) = %v, want muted", x, y, got)
										}
									}
								}
							}
						}
						if got := p.Foot(notices, f); !slices.Equal(got, wantKeys) {
							t.Errorf("hits = %+v, want %+v", got, wantKeys)
						}
						for y := 1; y < first; y++ {
							for x := 1; x < tc.inner+3; x++ {
								if got := buffer.Cell(x, y).Style.Bg; !footerSameColor(got, footBodyColor) {
									t.Errorf("body/title/padding background at (%d,%d) = %v, want warm fill", x, y, got)
								}
							}
						}
						for y := range view {
							for _, x := range []int{0, tc.inner + 3, tc.inner + 4, tc.inner + 5} {
								if got := buffer.Cell(x, y).Style.Bg; got != nil {
									t.Errorf("border/outside background at (%d,%d) = %v, want default", x, y, got)
								}
							}
						}
					})
				}
			}
		}
	}
}

func footerBareRows(mode presentation.GlyphMode, width int) []string {
	if mode == presentation.GlyphModeBasic {
		switch width {
		case 15:
			return []string{"r · u", "enter", "x", "esc/q"}
		case 12:
			return []string{"r", "u", "ent…", "x", "esc…"}
		default:
			return []string{"r", "u", "e…", "x", "e…"}
		}
	}
	switch width {
	case 15:
		return []string{"r · u", "↵ · x", "esc/q"}
	case 12:
		return []string{"r", "u", "↵", "x", "esc…"}
	default:
		return []string{"r", "u", "↵", "x", "e…"}
	}
}

func TestNoticeEmptyFooterBackgroundRoles(t *testing.T) {
	useTrueColor(t)
	f := Frame{Width: 20, Height: 8, Theme: footerColorTheme()}
	p := NewPanel(nil)
	view := p.View(nil, f)
	buffer := footerColorBuffer(view, 16)
	if got := footRows(view, 2); !slices.Equal(got, []string{"r refresh", "esc/q"}) {
		t.Fatalf("empty footer = %q", got)
	}
	for y := len(view) - 3; y < len(view); y++ {
		assertFooterColorRow(t, buffer, y, 16)
	}
	if got := buffer.Cell(2, 1).Style.Bg; !footerSameColor(got, footBodyColor) {
		t.Fatalf("empty message background = %v, want warm fill", got)
	}
}

// A filled footer is the original defect. Keep a negative control so the
// decoder must see its painted spaces as well as its colored text.
func TestNoticeFooterColorDecoderDetectsBodyFill(t *testing.T) {
	useTrueColor(t)
	theme := footerColorTheme()
	row := theme.Fill(" "+theme.Muted.Render("r · esc/q"), 12)
	buffer := footerColorBuffer([]string{row}, 12)
	for x := range 12 {
		if got := buffer.Cell(x, 0).Style.Bg; !footerSameColor(got, footBodyColor) {
			t.Fatalf("negative control background at %d = %v, want warm fill", x, got)
		}
	}
}

func footerColorBuffer(rows []string, width int) *cellbuf.Buffer {
	buffer := cellbuf.NewBuffer(width+2, len(rows))
	// Keep raw ANSI and place untouched padding after every row to catch leaks.
	cellbuf.SetContent(buffer, strings.Join(rows, "  \n")+"  ")
	return buffer
}

func assertFooterColorRow(t *testing.T, buffer *cellbuf.Buffer, y, width int) {
	t.Helper()
	for x := range width {
		if got := buffer.Cell(x, y).Style.Bg; got != nil {
			t.Errorf("footer background at (%d,%d) = %v, want default backdrop", x, y, got)
		}
	}
	for _, x := range []int{0, width - 1} {
		if got := buffer.Cell(x, y).Style.Fg; !footerSameColor(got, footBorderColor) {
			t.Errorf("border ink at (%d,%d) = %v, want %v", x, y, got, footBorderColor)
		}
	}
}

func footerSameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}
