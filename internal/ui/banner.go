package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The wordmark is drawn from half-block glyphs, three rows tall. Only the
// seven letters in "agent manager" exist, which is the whole point: a
// wordmark, not a font.
var bannerGlyphs = map[rune][3]string{
	'a': {"▄▀▄", "█▀█", "▀ ▀"},
	'g': {"█▀▀", "█▄█", "▀▀▀"},
	'e': {"█▀▀", "█▀▀", "▀▀▀"},
	'n': {"█▄ █", "█ ▀█", "▀  ▀"},
	't': {"▀█▀", " █ ", " ▀ "},
	'm': {"█▀▄▀█", "█ ▀ █", "▀   ▀"},
	'r': {"█▀█", "█▀▄", "▀ ▀"},
	' ': {"  ", "  ", "  "},
}

const (
	bannerWord = "agent manager"
	bannerRows = 3

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

// bannerWidth is the columns the wordmark needs, letters plus the single
// column between them.
func bannerWidth() int {
	width := 0
	for i, r := range bannerWord {
		if i > 0 {
			width++
		}
		width += ansi.StringWidth(bannerGlyphs[r][0])
	}
	return width
}

// showBanner reports whether the terminal is wide enough for the wordmark
// to sit beside the fleet rollup without crowding it.
func (m *Model) showBanner() bool {
	return m.width >= bannerWidth()+34
}

// headerRows is how many rows the header occupies, which the body height
// and every mouse hit-test are measured against.
func (m *Model) headerRows() int {
	if m.showBanner() {
		return bannerRows
	}
	return 1
}

// viewBanner draws the wordmark, lit by a highlight that sweeps left to
// right during the intro and then rests just past the end of the word.
func (m *Model) viewBanner() []string {
	sweep := float64(m.bannerPhase) / float64(bannerFrames)
	// The resting position keeps the tail of the word brightest, so a
	// settled header still has a direction to it.
	head := sweep * float64(bannerWidth()) * 1.25

	rows := make([]string, bannerRows)
	for row := 0; row < bannerRows; row++ {
		var b strings.Builder
		column := 0
		for i, letter := range bannerWord {
			if i > 0 {
				b.WriteString(" ")
				column++
			}
			glyph := bannerGlyphs[letter][row]
			for _, cell := range glyph {
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color(bannerTint(float64(column), head))).
					Render(string(cell)))
				column++
			}
		}
		rows[row] = strings.Repeat(" ", railGutter) + b.String()
	}
	return rows
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
