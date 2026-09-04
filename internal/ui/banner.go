package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The wordmark is the product name set in tracked-out capitals over the
// mark's own gradient, lit by a highlight that sweeps across it on launch
// and again once in a while. One row: a header, not a splash screen.
const (
	bannerWord = "agent manager"
	bannerRows = 1

	// bannerFrames is how long a sweep runs. Between sweeps the wordmark
	// holds still: a permanently animating header would repaint the frame
	// forever, and a repainting frame cannot be selected with the mouse.
	bannerFrames   = 22
	bannerInterval = 110 * time.Millisecond
	// bannerShimmerEvery re-runs the sweep on a settled wordmark.
	bannerShimmerEvery = 30 * time.Second

	// The wordmark's resting fill runs the brand teal into a soft blue,
	// pulled a step toward the theme's text so it stays legible on any
	// backdrop.
	bannerGradientFrom = "#12766a"
	bannerGradientTo   = "#6da4c8"
)

type (
	bannerTickMsg    struct{}
	bannerShimmerMsg struct{}
)

// bannerTick drives a sweep frame by frame; a settled wordmark waits out
// the shimmer interval before the next one.
func (m *Model) bannerTick() tea.Cmd {
	if m.bannerPhase >= bannerFrames {
		return tea.Tick(bannerShimmerEvery, func(time.Time) tea.Msg { return bannerShimmerMsg{} })
	}
	return tea.Tick(bannerInterval, func(time.Time) tea.Msg { return bannerTickMsg{} })
}

// bannerWidth is the columns the tracked-out wordmark needs.
func bannerWidth() int {
	return len(bannerWord)*2 - 1
}

// showBanner reports whether the terminal leaves the tracked-out wordmark
// room beside the fleet readings; a narrow one gets the name set plainly.
func (m *Model) showBanner() bool {
	return m.width >= bannerWidth()+railGutter+40
}

// headerRows is how many rows the header occupies, which the body height
// and every mouse hit-test are measured against.
func (m *Model) headerRows() int {
	if m.hideHeader {
		return 0
	}
	return bannerRows
}

// viewBanner draws the wordmark, lit by a highlight that sweeps left to
// right during the intro and then rests just past the end of the word.
func (m *Model) viewBanner() []string {
	sweep := float64(m.bannerPhase) / float64(bannerFrames)
	// The resting position keeps the tail of the word brightest, so a
	// settled header still has a direction to it.
	head := sweep * float64(bannerWidth()) * 1.25

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", railGutter))
	if !m.showBanner() {
		// Too tight for tracking; the name still leads the rail.
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(bannerWord))
		return []string{b.String()}
	}
	for i, letter := range bannerWord {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(bannerTint(float64(i*2), head))).
			Render(strings.ToUpper(string(letter))))
	}
	return []string{b.String()}
}

// bannerRest is a column's settled color: its slice of the mark's gradient.
func bannerRest(column float64) string {
	span := float64(bannerWidth() - 1)
	if span < 1 {
		span = 1
	}
	return mix(mix(bannerGradientFrom, bannerGradientTo, column/span), current.Text, 0.2)
}

// bannerTint colors one column of the wordmark by its distance from the
// sweep's head: lit at the head, its gradient color behind it, dimmed
// ahead of it so the sweep reads as light passing over the word.
func bannerTint(column, head float64) string {
	rest := bannerRest(column)
	distance := head - column
	switch {
	case distance < 0:
		return mix(rest, current.Subtle, 0.5)
	case distance < 4:
		return mix(rest, current.Bright, 1-distance/4)
	default:
		return rest
	}
}
