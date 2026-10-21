//go:build unix

package app

import (
	"os"
	"os/exec"
	"syscall"
)

// StartDetached starts this binary with args in a session of its own, so
// the SSH session or shell that asked for it can end without taking it
// along.
func StartDetached(args []string, logPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	return startDetached(exe, args, logPath)
}

func startDetached(exe string, args []string, logPath string) (int, error) {
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer log.Close()
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return 0, err
	}
	defer stdin.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}
