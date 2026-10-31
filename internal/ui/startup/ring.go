package startup

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// RingPoints is the number of dots the ring spends, so a phase wraps once
// per revolution.
const RingPoints = 12

// Ring is the loader that stands in for work in progress: a ring of dots
// with a lit head and a tail behind it, centered in width columns and
// carrying label under it. phase advances the head, wrapping once a
// revolution. A ring taller than height is cut to it; a shorter one is
// centered in it.
func Ring(g presentation.GlyphSet, width, height int, label string, phase int, t theme.Theme) []string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Working)).Bold(true)
	glow := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Working))
	rest := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Subtle))
	phase %= RingPoints
	dot := func(position int) string {
		switch position {
		case phase:
			return accent.Render(g.LoaderHead)
		case (phase + RingPoints - 1) % RingPoints:
			return glow.Render(g.LoaderTail)
		default:
			return rest.Render(g.LoaderRest)
		}
	}

	block := []string{
		presentation.CenterLine(dot(11)+"   "+dot(0)+"   "+dot(1), width),
		presentation.CenterLine(dot(10)+"       "+dot(2), width),
		presentation.CenterLine(dot(9)+"       "+dot(3), width),
		presentation.CenterLine(dot(8)+"       "+dot(4), width),
		presentation.CenterLine(dot(7)+"   "+dot(6)+"   "+dot(5), width),
		presentation.CenterLine(lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text)).Bold(true).Render(label), width),
	}
	if height <= len(block) {
		return block[:height]
	}
	lines := make([]string, height)
	copy(lines[(height-len(block))/2:], block)
	return lines
}
