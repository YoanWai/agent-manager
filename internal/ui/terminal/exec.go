package terminal

import (
	"io"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// ExecProcess hands the terminal to cmd for the length of its run.
//
// Bubble Tea's program output is wrapped for IME cursor placement, but
// os/exec only gives a child a TTY when Stdout is the actual *os.File.
func ExecProcess(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	return tea.Exec(childCommand{cmd}, fn)
}

// childCommand runs cmd on the terminal with the terminal's own pointer, and
// otherwise wires it the way tea.ExecProcess does.
type childCommand struct{ *exec.Cmd }

func (c childCommand) Run() error {
	ShowHand(false)
	return c.Cmd.Run()
}

func (c childCommand) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c childCommand) SetStdout(w io.Writer) {
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c childCommand) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}
