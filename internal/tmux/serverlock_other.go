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
