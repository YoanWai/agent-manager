package sessioncmd

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (b *Backend) Rename(ctx context.Context, sessionID, name string) (string, error) {
	runtime, err := b.resolve()
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is empty")
	}
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	interval := runtime.Config.PollInterval.Duration
	awake, err := runtime.Store.ManagerAwake(time.Now(), interval)
	if err != nil {
		return "", err
	}
	return renameMailbox(ctx, runtime.Hooks, sessionID, name, awake, interval)
}

func (b *Backend) ReviewRepo(sessionID, target string) (string, error) {
	runtime, err := b.resolve()
	if err != nil {
		return "", err
	}
	return reviewRepo(runtime.Hooks, sessionID, target)
}

func (b *Backend) ReviewBase(sessionID, cwd, ref string) (string, error) {
	runtime, err := b.resolve()
	if err != nil {
		return "", err
	}
	return reviewBase(runtime.Hooks, sessionID, cwd, ref)
}

func (b *Backend) ReviewScope(sessionID, scope string) (string, error) {
	runtime, err := b.resolve()
	if err != nil {
		return "", err
	}
	return reviewScope(runtime.Hooks, sessionID, scope)
}

func (b *Backend) ReviewComment(sessionID, commentID string, handled bool) (string, error) {
	runtime, err := b.resolve()
	if err != nil {
		return "", err
	}
	return reviewComment(runtime.Store, sessionID, commentID, handled)
}
