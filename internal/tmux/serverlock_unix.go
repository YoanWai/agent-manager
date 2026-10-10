//go:build darwin || linux

package tmux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockServer holds the lock file beside the server's socket, shared or
// exclusive, until unlock.
func lockServer(socket string, exclusive bool) (unlock func(), err error) {
	return lockServerContext(context.Background(), socket, exclusive)
}

func lockServerContext(ctx context.Context, socket string, exclusive bool) (unlock func(), err error) {
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
		err = syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			file.Close()
			return nil, fmt.Errorf("tmux server lock %s: %w", path, err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return func() { file.Close() }, nil
}
