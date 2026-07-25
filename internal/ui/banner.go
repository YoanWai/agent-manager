package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The wordmark is the product name set in tracked-out capitals, lit by a
// highlight that sweeps across it once on launch. One row: a header, not a
// splash screen.
const (
	bannerWord = "agent manager"
	bannerRows = 1

	// bannerFrames is how long the intro sweep runs. The wordmark animates
	// on launch and then holds still: a permanently animating header would
	// repaint the frame forever, and a repainting frame cannot be selected
	// with the mouse.
	bannerFrames   = 22
	bannerInterval = 110 * time.Millisecond
)

type bannerTickMsg struct{}

// bannerTick drives the intro sweep, and stops scheduling itself once the
// wordmark has settled.
func (m *Model) bannerTick() tea.Cmd {
	if m.bannerPhase >= bannerFrames {
		return nil
	}
	return tea.Tick(bannerInterval, func(time.Time) tea.Msg { return bannerTickMsg{} })
}

// bannerWidth is the columns the tracked-out wordmark needs.
func bannerWidth() int {
	return len(bannerWord)*2 - 1
}

// showBanner reports whether the rail is wide enough for the tracked-out
// wordmark; a narrow rail gets the name set plainly instead.
func (m *Model) showBanner() bool {
	left, _ := m.splitWidths()
	return left >= bannerWidth()+railGutter+1
}

// headerRows is how many rows the header occupies, which the body height
// and every mouse hit-test are measured against.
func (m *Model) headerRows() int { return bannerRows }

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

// bannerTint colors one column of the wordmark by its distance from the
// sweep's head: lit at the head, accent behind it, and the quieter
// secondary accent ahead of it.
func bannerTint(column, head float64) string {
	distance := head - column
	switch {
	case distance < 0:
		return mix(current.Accent2, current.Subtle, 0.55)
	case distance < 4:
		return mix(current.Accent, current.Bright, 1-distance/4)
	case distance < 12:
		return mix(current.Accent, current.Accent2, (distance-4)/8)
	default:
		return current.Accent2
	}
}
