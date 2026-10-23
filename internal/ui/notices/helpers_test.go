package notices

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func useTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// testTheme gives each role its own color, so a test can tell them apart.
func testTheme() Theme {
	return Theme{
		Accent:      lipgloss.Color("#00a0ff"),
		Accent2:     lipgloss.Color("#a000ff"),
		Bright:      lipgloss.Color("#ffffff"),
		SubtleColor: lipgloss.Color("#808080"),
		Subtle:      lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
		Muted:       lipgloss.NewStyle().Foreground(lipgloss.Color("#a0a0a0")),
		Value:       lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Key:         lipgloss.NewStyle().Foreground(lipgloss.Color("#00ffa0")),
		Border:      lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")),
		Title:       lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")).Bold(true),
		Fill:        presentation.PadRight,
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
