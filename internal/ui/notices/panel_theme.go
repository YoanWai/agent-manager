package notices

import (
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// PanelTheme is the messages card's palette: the manager's own tones on the
// rail's fill, one step above the backdrop, framed in the accent.
func PanelTheme() Theme {
	return Theme{
		Accent:   uitheme.ColorAccent(),
		Accent2:  uitheme.ColorAccent2(),
		Bright:   uitheme.ColorBright(),
		Working:  uitheme.ColorWorking(),
		Waiting:  uitheme.ColorWaiting(),
		Finished: uitheme.ColorFinished(),
		Ink:      uitheme.ColorBg(),
		Subtle:   uitheme.SubtleStyle(),
		Muted:    uitheme.MutedStyle(),
		Value:    uitheme.ValueStyle(),
		Key:      uitheme.KeyStyle(),
		Border:   lipgloss.NewStyle().Foreground(uitheme.ColorAccent()),
		Title:    lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true),
		Fill:     func(row string, width int) string { return uitheme.Paint(row, width, uitheme.PanelHex()) },
		Surface:  func(row string, width int) string { return uitheme.Paint(row, width, uitheme.Current().Surface) },
		Raised:   func(row string, width int) string { return uitheme.Paint(row, width, uitheme.Current().Subtle) },
		Callout: func(row string, width int, tone lipgloss.Color) string {
			return uitheme.Paint(row, width, uitheme.Mix(uitheme.PanelHex(), string(tone), 0.14))
		},
	}
}
