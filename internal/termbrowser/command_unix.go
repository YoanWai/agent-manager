//go:build darwin || linux

package termbrowser

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
		// group is signaled until no live member is left. macOS answers EPERM
		// once only exited members remain, Linux ESRCH once Wait reaped them.
		deadline := time.Now().Add(100 * time.Millisecond)
		for {
			err := syscall.Kill(group, syscall.SIGKILL)
			if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) {
				return nil
			}
			if err != nil {
				return err
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("process group %d still has members 100ms after SIGKILL", -group)
			}
			time.Sleep(time.Millisecond)
		}
	}
}
