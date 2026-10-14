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

// termGroup asks the whole process group to stop.
func termGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// killGroup kills the whole process group.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
