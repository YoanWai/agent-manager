//go:build darwin

package systheme

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
)

// discardInput drops replies the tty received but nobody read, so a reply
// that lands after its read gave up never reaches the TUI as keystrokes. A
// descriptor that is not a tty, as in the tests, has nothing to drop.
func discardInput(fd int) { _ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fread) }

// fread is <sys/fcntl.h>'s FREAD, the queue TIOCFLUSH drops: what tcflush
// sends for TCIFLUSH. x/sys/unix does not export it.
const fread = 0x1
