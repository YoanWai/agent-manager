//go:build darwin || linux

package tmux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	return lockFileContext(ctx, "tmux server lock", socketPathFromEnv(socket)+".attach-gate", exclusive)
}

// lockPaneSend holds one pane's send lock across processes: a manager's
// poller and an `agent-manager type` run as separate processes on one host,
// and the in-process gate alone cannot keep their pastes apart.
func lockPaneSend(ctx context.Context, socket, target string) (unlock func(), err error) {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, target)
	return lockFileContext(ctx, "pane send lock", socketPathFromEnv(socket)+".send-"+name, true)
}

func lockFileContext(ctx context.Context, what, path string, exclusive bool) (unlock func(), err error) {
	// tmux makes this directory on first use, and the lock can come first.
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
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
			return nil, fmt.Errorf("%s %s: %w", what, path, err)
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
