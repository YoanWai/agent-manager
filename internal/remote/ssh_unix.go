//go:build unix

package remote

import (
	"os/exec"
	"syscall"
)

// killGroup runs ssh in a process group of its own and ends the whole group
// at the deadline, since a ProxyCommand or ProxyJump helper outlives a
// killed ssh.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
