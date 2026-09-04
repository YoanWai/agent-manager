package ui

import (
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubble Tea's program output is wrapped for IME cursor placement, but
// os/exec only gives a child a TTY when Stdout is the actual *os.File.
func execTerminalProcess(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	return tea.ExecProcess(cmd, fn)
}
