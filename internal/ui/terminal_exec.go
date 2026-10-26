package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"os"
	"os/exec"
)

// Bubble Tea's program output is wrapped for IME cursor placement, but
// os/exec only gives a child a TTY when Stdout is the actual *os.File.
func execTerminalProcess(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	return execTerminal(processCommand{cmd}, fn)
}

// execTerminal hands the terminal to c with mode 2031 off: Bubble Tea
// releases only the modes it set, so a scheme flip would otherwise reach
// the child as typed input. Re-enabling makes the terminal report again,
// which catches up on a flip made while the child ran.
func execTerminal(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
	return tea.Exec(schemeReportsPaused{c}, fn)
}

type schemeReportsPaused struct{ tea.ExecCommand }

func (c schemeReportsPaused) Run() error {
	_ = DisableColorSchemeReports()
	defer func() { _ = EnableColorSchemeReports() }()
	return c.ExecCommand.Run()
}

// processCommand mirrors the adapter tea.ExecProcess builds, which Bubble
// Tea does not export: a stream the caller already set is kept.
type processCommand struct{ *exec.Cmd }

func (c processCommand) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c processCommand) SetStdout(w io.Writer) {
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c processCommand) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}
