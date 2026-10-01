//go:build !windows

package catalog

import (
	"os/exec"
	"syscall"
)

// startProcessGroup puts a server's children in the server's own process
// group, so stop reaches a forked child too.
func startProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup TERMs a whole process group, which stop follows with a direct
// KILL of the server itself if the group has not exited by then.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}
