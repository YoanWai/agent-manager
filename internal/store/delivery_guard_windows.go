//go:build windows

package store

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func tryDeliveryFileLock(path string) (unlock func(), acquired bool, err error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open delivery guard: %w", err)
	}
	// Like flock, the lock belongs to this handle: another open of the same
	// file, in this process or another, is refused until the handle closes.
	err = windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		file.Close()
		return nil, false, nil
	}
	if err != nil {
		file.Close()
		return nil, false, fmt.Errorf("lock delivery guard: %w", err)
	}
	return func() { _ = file.Close() }, true, nil
}
