package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amuitest"

// TestMain kills any leftover test server so each run starts and ends clean.
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it
// ("server exited unexpectedly").
func TestMain(m *testing.M) {
	// A copy of this binary a pane launched as a test tool's stand-in runs
	// that fixture, not the suite.
	if code, ok := runFixture(os.Args); ok {
		os.Exit(code)
	}
	// A copy of this binary launched as the notifier helper must act as
	// one, or it reruns the whole suite and kills the parent run's server.
	if notify.LaunchedAsHelper() {
		os.Exit(notify.HelperMain(os.Args[1:]))
	}
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	// psmux keeps its servers' registry under its data directory, which a
	// fresh one isolates the way the private socket does for tmux.
	if runtime.GOOS == "windows" {
		dir, err := os.MkdirTemp("", "amuitest-psmux-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "psmux data dir: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		os.Setenv("PSMUX_DATA_DIR", dir)
		bin, err := installFixtures()
		if err != nil {
			fmt.Fprintf(os.Stderr, "test tool fixtures: %v\n", err)
			return 1
		}
		defer os.RemoveAll(bin)
	}
	// The suite runs as if at the machine it runs on, even when the
	// developer reached it over SSH.
	remoteTerminal = localTerminal
	// kill-server fails whenever no server is up, which is the normal case.
	tmuxCmd("kill-server").Run()
	// Without tmux the run still starts: each test skips through its own
	// requireTmux. With tmux, a run that could not plant the anchor would
	// pass or flake on luck, so it stops instead.
	if _, err := exec.LookPath(tmux.Binary); err == nil {
		if out, err := tmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor session: %v: %s\n", err, out)
			return 1
		}
	}
	// A real banner on macOS builds that helper from this binary.
	postNotification = func(notify.Event) {}
	code := m.Run()
	tmuxCmd("kill-server").Run()
	return code
}

// tmuxCmd builds a raw tmux command aimed at the test socket, matching the
// socket buildModel's driver runs on.
func tmuxCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(tmux.Binary, append([]string{"-L", testSocket}, args...)...)
	if runtime.GOOS == "windows" {
		cmd.Env = append(os.Environ(), "PSMUX_NO_WARM=1")
	}
	return cmd
}

// sessionTmuxCmd aims a server-scoped command (a global option, a binding)
// at the server hosting session id. tmux has one server for all sessions;
// psmux runs one per session and is told which with a leading -t.
func sessionTmuxCmd(id string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		args = append([]string{"-t", "am_" + id}, args...)
	}
	return tmuxCmd(args...)
}
