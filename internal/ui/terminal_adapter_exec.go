package ui

import (
	"os/exec"

	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

func execTerminalProcess(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return uiterminal.ExecProcess(cmd, fn)
}

func DisableAlternateScroll() error { return uiterminal.DisableAlternateScroll() }
