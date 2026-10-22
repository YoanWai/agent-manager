//go:build !darwin && !linux

package tmux

import "context"

// Elsewhere attachGate alone orders tmux commands.
func lockServer(string, bool) (func(), error) {
	return func() {}, nil
}

func lockServerContext(context.Context, string, bool) (func(), error) {
	return func() {}, nil
}

// Remote hosts are Linux or macOS, the only place a second process types
// into a pane, so elsewhere the in-process gate is the whole lock.
func lockPaneSend(context.Context, string, string) (func(), error) {
	return func() {}, nil
}
