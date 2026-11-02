package notices

import (
	"fmt"
	"image/color"
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
	t.Accent = "#00ffaf"
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
			for _, width := range []int{10, 12, 15, 20, 30, 80, 160} {
				for _, id := range []string{"", "custom", "update-test"} {
					t.Run(fmt.Sprintf("%d/%s/%d/%s", profile, mode, width, id), func(t *testing.T) {
						p := NewPanel(nil, nil)
						f := testFrame()
						f.Width = width
						f.GlyphMode = mode
						f.Theme = footerColorTheme()
						notices := footNotices(id)
						view := p.View(notices, f)
						cols := ansi.StringWidth(view[0])
						buffer := footerColorBuffer(view, cols)
						footerRows := map[int]bool{len(view) - 1: true}
						for _, hit := range p.Foot(notices, f) {
							y := len(view) - 1 + hit.Y
							footerRows[y] = true
							labelEnd := hit.X0 + ansi.StringWidth(presentation.KeyLabel(mode, hit.Label))
							if labelEnd+1 < hit.X1 {
								if got := buffer.Cell(labelEnd+1, y).Style.Fg; !footerSameColor(got, footMutedColor) {
									t.Fatalf("action word ink=%v", got)
								}
							}
							if got := buffer.Cell(hit.X0, y).Style.Fg; !footerSameColor(got, footKeyColor) {
								t.Fatalf("key ink=%v", got)
							}
						}
						for y := range footerRows {
							assertFooterColorRow(t, buffer, y, cols)
						}
						bodyPaint := false
						for y := 1; y < len(view)-1; y++ {
							if footerRows[y] {
								continue
							}
							for x := 1; x < cols-1; x++ {
								if footerSameColor(buffer.Cell(x, y).Style.Bg, footBodyColor) {
									bodyPaint = true
								}
							}
						}
						if !bodyPaint {
							t.Fatal("message body lost its fill")
						}
					})
				}
			}
		}
	}
}
func TestNoticeFooterColorDecoderDetectsBodyFill(t *testing.T) {
	useTrueColor(t)
	tint := footerColorTheme()
	buffer := footerColorBuffer([]string{tint.Fill("body", 10)}, 10)
	if !footerSameColor(buffer.Cell(5, 0).Style.Bg, footBodyColor) {
		t.Fatal("decoder missed filled spaces")
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
