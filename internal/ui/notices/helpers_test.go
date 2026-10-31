package notices

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"strings"
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
		Accent:   lipgloss.Color("#00a0ff"),
		Accent2:  lipgloss.Color("#a000ff"),
		Bright:   lipgloss.Color("#ffffff"),
		Working:  lipgloss.Color("#ff8000"),
		Waiting:  lipgloss.Color("#c080ff"),
		Finished: lipgloss.Color("#00c040"),
		Ink:      lipgloss.Color("#101010"),
		Subtle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#606060")),
		Muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("#a0a0a0")),
		Value:    lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")),
		Key:      lipgloss.NewStyle().Foreground(lipgloss.Color("#00ffa0")),
		Border:   lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")),
		Title:    lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")).Bold(true),
		Fill:     presentation.PadRight,
		Surface:  presentation.PadRight,
		Raised:   presentation.PadRight,
		Callout:  func(row string, width int, _ lipgloss.Color) string { return presentation.PadRight(row, width) },
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
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func rowIndex(rows []string, want string) int {
	for index, row := range rows {
		if strings.Contains(row, want) {
			return index
		}
	}
	return -1
}

func footerLegend(row string, mode presentation.GlyphMode) string {
	left, right := "╰", "╯"
	if mode == presentation.GlyphModeBasic {
		left, right = "+", "+"
	}
	return strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(row, left+"─"), right), "─")
}

func bindingOf(t *testing.T, text string) keybind.Binding {
	t.Helper()
	key, err := keybind.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return keybind.Keys(key)
}
