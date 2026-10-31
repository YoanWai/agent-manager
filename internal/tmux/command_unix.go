//go:build darwin || linux

package tmux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureBoundedCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		group := -cmd.Process.Pid
		err := syscall.Kill(group, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err != nil {
			return err
		}
		// On macOS a child forked while the signal goes out can miss it, so the
		// group is signaled until Wait has reaped the leader and nothing is left.
		deadline := time.Now().Add(commandTimeout)
		for syscall.Kill(group, syscall.SIGKILL) == nil {
			if time.Now().After(deadline) {
				return fmt.Errorf("process group %d still running %v after SIGKILL", -group, commandTimeout)
			}
			time.Sleep(time.Millisecond)
		}
		return nil
	}
}
