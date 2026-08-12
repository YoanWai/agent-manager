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
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it
// ("server exited unexpectedly", the recurring CI failure in
// TestFocusWatchReportsCursor).
func TestMain(m *testing.M) {
	tmuxCmd("kill-server").Run()
	tmuxCmd("new-session", "-d", "-s", "anchor").Run()
	code := m.Run()
	tmuxCmd("kill-server").Run()
	os.Exit(code)
}

// tmuxCmd builds a raw tmux command aimed at the test socket, matching the
// socket buildModel's driver runs on.
func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", testSocket}, args...)...)
}
