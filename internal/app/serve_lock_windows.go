//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func tryServeLock(path string) (release func(), acquired bool, err error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open serve lock: %w", err)
	}
	// Like flock, the lock belongs to this handle and ends when it closes,
	// including when the process holding it dies.
	err = windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		file.Close()
		return nil, false, nil
	}
	if err != nil {
		file.Close()
		return nil, false, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() { _ = file.Close() }, true, nil
}
