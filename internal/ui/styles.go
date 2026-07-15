package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
)

var (
	colorBg      = lipgloss.Color("236")
	colorMuted   = lipgloss.Color("244")
	colorAccent  = lipgloss.Color("111")
	colorWorking = lipgloss.Color("214")
	colorReady   = lipgloss.Color("42")
	colorErrored = lipgloss.Color("203")
	colorIdle    = lipgloss.Color("244")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(colorAccent).
			Padding(0, 1)

	groupHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorBg).
				Foreground(lipgloss.Color("231"))

	rowStyle = lipgloss.NewStyle()

	mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().Foreground(colorMuted)

	errStyle = lipgloss.NewStyle().Foreground(colorErrored).Bold(true)

	labelStyle = lipgloss.NewStyle().Foreground(colorMuted)
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("231"))
)

func statusColor(s string) lipgloss.Color {
	switch s {
	case status.Working:
		return colorWorking
	case status.Ready:
		return colorReady
	case status.Errored, status.Dead:
		return colorErrored
	default:
		return colorIdle
	}
}

func statusGlyph(s string) string {
	switch s {
	case status.Working:
		return "●"
	case status.Ready:
		return "●"
	case status.Errored, status.Dead:
		return "✖"
	default:
		return "○"
	}
}
