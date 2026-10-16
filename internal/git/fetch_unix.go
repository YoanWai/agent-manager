//go:build !windows

package git

import (
	"os/exec"
	"syscall"
)

// killFetchTree runs the fetch in a session of its own and cancels it by
// killing that whole group, so an ssh git started is not left holding the
// connection.
func killFetchTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
