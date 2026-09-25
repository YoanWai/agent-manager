//go:build darwin || linux

package tmux

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// lockServer holds the lock file beside the server's socket, shared or
// exclusive, until unlock.
func lockServer(socket string, exclusive bool) (unlock func(), err error) {
	// tmux takes <socket>.lock itself while it starts a server.
	path := socketPathFromEnv(socket) + ".attach-gate"
	// tmux makes this directory on first use, and the lock can come first.
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("tmux server lock: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tmux server lock: %w", err)
	}
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	for {
		err = syscall.Flock(int(file.Fd()), how)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("tmux server lock %s: %w", path, err)
	}
	return func() { file.Close() }, nil
}
