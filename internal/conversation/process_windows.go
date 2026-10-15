//go:build windows

package conversation

import (
	"errors"
	"math"
	"os"

	"golang.org/x/sys/windows"
)

// Alive reports whether a process runs under pid. A process that exited
// keeps its pid while a handle to it stays open, so its exit code says
// whether it still runs.
func Alive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}

// lockFile takes an exclusive lock on file, held until it is closed. A
// Windows byte-range lock blocks other handles' reads and writes inside its
// range, so the lock sits on a byte near the largest offset Windows takes,
// which no writer reaches, and only another lockFile waits on it.
func lockFile(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{Offset: math.MaxUint32 - 1, OffsetHigh: math.MaxInt32})
}
