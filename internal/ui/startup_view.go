package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

const startupRingPoints = 12

func (m *Model) startupLoader(width, height int) []string {
	sess, ok := m.selected()
	if !ok || m.mode == modeFocus || sess.Status != status.Starting || paneBooted(m.workspace.preview) {
		return nil
	}
	return ringLoader(m.glyphs(), width, height, "starting up", m.startup.startupPhase)
}

func ringLoader(g presentation.GlyphSet, width, height int, label string, phase int) []string {
	accent := lipgloss.NewStyle().Foreground(statusColor(status.Starting)).Bold(true)
	glow := lipgloss.NewStyle().Foreground(statusColor(status.Starting))
	phase = phase % startupRingPoints
	dot := func(position int) string {
		switch position {
		case phase:
			return accent.Render(g.LoaderHead)
		case (phase + startupRingPoints - 1) % startupRingPoints:
			return glow.Render(g.LoaderTail)
		default:
			return subtleStyle().Render(g.LoaderRest)
		}
	}

	block := []string{
		presentation.CenterLine(dot(11)+"   "+dot(0)+"   "+dot(1), width),
		presentation.CenterLine(dot(10)+"       "+dot(2), width),
		presentation.CenterLine(dot(9)+"       "+dot(3), width),
		presentation.CenterLine(dot(8)+"       "+dot(4), width),
		presentation.CenterLine(dot(7)+"   "+dot(6)+"   "+dot(5), width),
		presentation.CenterLine(valueStyle().Bold(true).Render(label), width),
	}
	if height <= len(block) {
		return block[:height]
	}
	lines := make([]string, height)
	copy(lines[(height-len(block))/2:], block)
	return lines
}
