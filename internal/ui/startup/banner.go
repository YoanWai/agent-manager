// Package startup paints what the manager says while it comes up: the
// wordmark over the rail, lit by a sweep that crosses it once, and the ring
// that stands in for a session still booting. It is the pure half of the
// startup state; the tick that drives a frame stays on the root.
package startup

import (
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// The wordmark is the product name set in tracked-out capitals over the
// mark's own gradient, lit by a highlight that sweeps across it on launch
// and again once in a while. One row: a header, not a splash screen.
const (
	Word = "agent manager"
	Rows = 1

	// Frames is how long a sweep runs. Between sweeps the wordmark holds
	// still: a permanently animating header would repaint the frame forever,
	// and a repainting frame cannot be selected with the mouse.
	Frames = 22
	// Interval is the gap between two frames of a sweep.
	Interval = 110 * time.Millisecond
	// ShimmerEvery re-runs the sweep on a settled wordmark.
	ShimmerEvery = 30 * time.Second

	// The wordmark's resting fill runs the brand teal into a soft blue,
	// pulled a step toward the theme's text so it stays legible on any
	// backdrop.
	gradientFrom = "#12766a"
	gradientTo   = "#6da4c8"
)

// Width is the columns the tracked-out wordmark needs.
func Width() int { return len(Word)*2 - 1 }

// Fits reports whether a terminal of width columns leaves the tracked-out
// wordmark room beside the fleet readings; a narrow one gets the name set
// plainly.
func Fits(width, gutter int) bool { return width >= Width()+gutter+40 }

// Line draws the wordmark for phase of Frames, indented by gutter: one row,
// painted on t. A terminal too narrow for the tracking gets the name bold
// and untracked, still leading the rail.
func Line(phase, gutter int, tracked bool, t theme.Theme) []string {
	if !tracked {
		return []string{strings.Repeat(" ", gutter) +
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Accent)).Render(Word)}
	}
	sweep := float64(phase) / float64(Frames)
	// The resting position keeps the tail of the word brightest, so a
	// settled header still has a direction to it.
	head := sweep * float64(Width()) * 1.25

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", gutter))
	for i, letter := range Word {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(Tint(float64(i*2), head, t))).
			Render(strings.ToUpper(string(letter))))
	}
	return []string{b.String()}
}

// Rest is a column's settled color: its slice of the mark's gradient, pulled
// toward the theme's text.
func Rest(column float64, t theme.Theme) string {
	span := float64(Width() - 1)
	if span < 1 {
		span = 1
	}
	return theme.Mix(theme.Mix(gradientFrom, gradientTo, column/span), t.Text, 0.2)
}

// Tint colors one column of the wordmark by its distance from the sweep's
// head: lit at the head, its gradient color behind it, dimmed ahead of it so
// the sweep reads as light passing over the word.
func Tint(column, head float64, t theme.Theme) string {
	rest := Rest(column, t)
	distance := head - column
	switch {
	case distance < 0:
		return theme.Mix(rest, t.Subtle, 0.5)
	case distance < 4:
		return theme.Mix(rest, t.Bright, 1-distance/4)
	default:
		return rest
	}
}
