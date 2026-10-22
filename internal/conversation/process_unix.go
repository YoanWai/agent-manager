//go:build !windows

package conversation

import (
	"errors"
	"os"
	"syscall"
)

// Alive reports whether a process runs under pid.
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// lockFile takes an exclusive lock on file, held until it is closed.
func lockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}
