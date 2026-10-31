package notices

import (
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Theme is the messages card's palette: the manager's own tones, with the
// card fill, its border and its title pulled toward the same warm yellow so
// the three read as one object.
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
		Border:   noticeBorderStyle(),
		Title:    noticeTitleStyle(),
		Fill:     func(row string, width int) string { return uitheme.Paint(row, width, noticeCardHex()) },
		Surface:  func(row string, width int) string { return uitheme.Paint(row, width, uitheme.Current().Surface) },
		Callout:  func(row string, width int) string { return uitheme.Paint(row, width, calloutHex()) },
	}
}

// noticeCardHex is the messages modal's fill: the panel tone warmed toward
// yellow.
func noticeCardHex() string { return uitheme.Mix(uitheme.PanelHex(), "#e2c044", 0.16) }

func calloutHex() string { return uitheme.Mix(noticeCardHex(), uitheme.Current().Accent, 0.14) }

// noticeBorderStyle is the card's rounded frame, dimmed toward the same
// yellow so the outline and the fill read as one object.
func noticeBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.Mix(uitheme.Current().Subtle, "#e2c044", 0.45)))
}

func noticeTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.Mix(uitheme.Current().Bright, "#e2c044", 0.5))).Bold(true)
}
