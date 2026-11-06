//go:build linux

package systheme

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios  = unix.TCGETS
	ioctlWriteTermios = unix.TCSETS
)

// discardInput drops replies the tty received but nobody read, so a reply
// that lands after its read gave up never reaches the TUI as keystrokes. A
// descriptor that is not a tty, as in the tests, has nothing to drop.
func discardInput(fd int) { _ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH) }
