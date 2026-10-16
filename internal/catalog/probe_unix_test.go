//go:build !windows

package catalog

import "syscall"

// processAlive reports whether a pid still has a process behind it, through
// the POSIX signal-0 probe.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
