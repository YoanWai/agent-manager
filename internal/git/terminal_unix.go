//go:build !windows

package git

import (
	"os/exec"
	"syscall"
)

// detachTerminal starts git in a session of its own, with no controlling
// terminal for a prompt to reach.
func detachTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
