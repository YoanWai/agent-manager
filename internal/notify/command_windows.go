//go:build windows

package notify

import (
	"context"
	"os/exec"
	"strconv"

	"github.com/YoanWai/agent-manager/internal/pwsh"
)

// shellCommand runs the notify command through PowerShell, the shell the
// panes run on Windows. The command travels encoded, so its quotes reach
// PowerShell as written rather than through the command-line parser.
func shellCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	shell, err := pwsh.Path()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-EncodedCommand", pwsh.EncodedCommand(command)), nil
}

// killTreeOnCancel cancels the command by killing PowerShell and every
// process under it.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
