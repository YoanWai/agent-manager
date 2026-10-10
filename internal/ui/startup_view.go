package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
)

var startupFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const startupRingPoints = 12

func (m *Model) startupLoader(width, height int) []string {
	sess, ok := m.selected()
	if !ok || m.mode == modeFocus || sess.Status != status.Starting || paneBooted(m.workspace.preview) {
		return nil
	}
	return ringLoader(width, height, "starting up", m.startup.startupPhase)
}

func ringLoader(width, height int, label string, phase int) []string {
	accent := lipgloss.NewStyle().Foreground(statusColor(status.Starting)).Bold(true)
	glow := lipgloss.NewStyle().Foreground(statusColor(status.Starting))
	phase = phase % startupRingPoints
	dot := func(position int) string {
		switch position {
		case phase:
			return accent.Render("●")
		case (phase + startupRingPoints - 1) % startupRingPoints:
			return glow.Render("•")
		default:
			return subtleStyle.Render("·")
		}
	}

	block := []string{
		centerLine(dot(11)+"   "+dot(0)+"   "+dot(1), width),
		centerLine(dot(10)+"       "+dot(2), width),
		centerLine(dot(9)+"       "+dot(3), width),
		centerLine(dot(8)+"       "+dot(4), width),
		centerLine(dot(7)+"   "+dot(6)+"   "+dot(5), width),
		centerLine(valueStyle.Bold(true).Render(label), width),
	}
	if height <= len(block) {
		return block[:height]
	}
	lines := make([]string, height)
	copy(lines[(height-len(block))/2:], block)
	return lines
}
