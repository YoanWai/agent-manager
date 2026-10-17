//go:build darwin || linux

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func tryDeliveryFileLock(path string) (unlock func(), acquired bool, err error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open delivery guard: %w", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		file.Close()
		return nil, false, nil
	}
	if err != nil {
		file.Close()
		return nil, false, fmt.Errorf("lock delivery guard: %w", err)
	}
	return func() { _ = file.Close() }, true, nil
}
