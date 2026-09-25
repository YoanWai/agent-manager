//go:build !darwin && !linux

package tmux

// Elsewhere attachGate alone orders tmux commands.
func lockServer(string, bool) (func(), error) {
	return func() {}, nil
}
