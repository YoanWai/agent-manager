package ui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/charmbracelet/x/ansi"
)

// reportLaunchError routes a failed spawn to the right surface: a launch
// the manager refused for a missing prerequisite opens a dialog naming the
// command that unblocks it, anything else stays a line of status text.
func (m *Model) reportLaunchError(err error) {
	if errors.Is(err, mcpreg.ErrHermesMCPUnavailable) {
		m.launchHint = "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
			"Run `hermes setup` to install MCP support, then spawn again."
		m.mode = modeLaunchHint
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.launchHint = missingToolHint(missing)
		m.mode = modeLaunchHint
		return
	}
	m.errBar.text = err.Error()
}

func missingToolHint(missing config.MissingToolError) string {
	return missing.Binary + " is not installed.\n\n" + deps.Hint(missing.Binary)
}

// Any key closes the dialog, the way the confirm dialog reads every
// answer but yes as no: there is nothing to decide here, only to read.
func (m *Model) handleLaunchHintKey(tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.launchHint = ""
	m.mode = modeList
	return m, nil
}

func (m *Model) viewLaunchHint() string {
	width := m.cardWidth()
	inner := cardInnerWidth(width)
	tone := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	var body strings.Builder
	for i, paragraph := range strings.Split(m.launchHint, "\n\n") {
		style := mutedStyle
		if i == 0 {
			style = tone
		}
		if i > 0 {
			body.WriteString("\n")
		}
		for _, line := range strings.Split(ansi.Wordwrap(paragraph, inner, "-"), "\n") {
			body.WriteString(style.Render(line) + "\n")
		}
	}
	hint := [][2]string{{"any key", "close"}}
	return m.cardSized(width, "◈ Session needs a setup step", strings.TrimRight(body.String(), "\n"), hint)
}
