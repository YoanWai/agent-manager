package watch

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amwatchtest"

// TestMain kills any leftover test server so each run starts and ends clean.
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it.
func TestMain(m *testing.M) {
	if code, ok := testfixture.Run(os.Args); ok {
		os.Exit(code)
	}
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	cleanup, err := testfixture.Prepare(testSocket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()
	// kill-server fails whenever no server is up, which is the normal case.
	tmuxCmd("kill-server").Run()
	if _, err := exec.LookPath(tmux.Binary); err == nil {
		if out, err := tmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor session: %v: %s\n", err, out)
			return 1
		}
	}
	code := m.Run()
	tmuxCmd("kill-server").Run()
	return code
}

func tmuxCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(tmux.Binary, append([]string{"-L", testSocket}, args...)...)
	if runtime.GOOS == "windows" {
		cmd.Env = append(os.Environ(), "PSMUX_NO_WARM=1")
	}
	return cmd
}

func requireDriver(t *testing.T) *tmux.Driver {
	t.Helper()
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("NewWithSocket: %v", err)
	}
	return driver
}

// skipControlMode skips a test that needs the watcher's control client up:
// on Windows OpenControl refuses by design and previews poll instead.
func skipControlMode(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the manager does not use psmux control mode")
	}
}

// lostMsg is a lost client as newTestWatcher delivers it.
type lostMsg struct{ err error }

// newTestWatcher delivers frames and lost clients through one send, the way
// the root UI receives them as messages.
func newTestWatcher(driver *tmux.Driver, send func(any)) *Watcher {
	return New(driver, 300*time.Millisecond, Sink{
		Frame: func(frame Frame) { send(frame) },
		Lost:  func(err error) { send(lostMsg{err}) },
	})
}
