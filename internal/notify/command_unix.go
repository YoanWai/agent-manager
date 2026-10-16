//go:build !windows

package notify

import (
	"context"
	"os/exec"
	"syscall"
)

// shellCommand runs the notify command through sh -c.
func shellCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, "sh", "-c", command), nil
}

// killTreeOnCancel runs the command in a session of its own and cancels it
// by killing that whole group.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
