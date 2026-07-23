package ui

import (
	"os"
	"os/exec"
	"testing"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amuitest"

// TestMain kills any leftover test server so each run starts and ends clean.
func TestMain(m *testing.M) {
	tmuxCmd("kill-server").Run()
	code := m.Run()
	tmuxCmd("kill-server").Run()
	os.Exit(code)
}

// tmuxCmd builds a raw tmux command aimed at the test socket, matching the
// socket buildModel's driver runs on.
func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", testSocket}, args...)...)
}
