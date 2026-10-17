package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run. A test's models run
// on a server of their own (testServer); this one hosts the tests that drive
// tmux without a model.
const testSocket = "amuitest"

// TestMain kills any leftover test server so each run starts and ends clean.
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it
// ("server exited unexpectedly").
func TestMain(m *testing.M) {
	// A copy of this binary a pane launched as a test tool's stand-in runs
	// that fixture, not the suite.
	if code, ok := testfixture.Run(os.Args); ok {
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
	cleanup, err := testfixture.Prepare(testSocket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer cleanup()
	removeTemplate, err := prepareStoreTemplate()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer removeTemplate()
	// The suite runs as if at the machine it runs on, even when the
	// developer reached it over SSH.
	remoteTerminal = localTerminal
	// Panes fall back to $SHELL once their agent exits, and the test server
	// takes it as its default-shell. A developer's interactive shell runs
	// startup files that fork for seconds, which AgentRunning reads as an
	// agent, so the suite runs the plain sh CI runs.
	if runtime.GOOS != "windows" {
		os.Setenv("SHELL", "/bin/sh")
	}
	// kill-server fails whenever no server is up, which is the normal case.
	sharedTmuxCmd("kill-server").Run()
	// Without tmux the run still starts: each test skips through its own
	// requireTmux. With tmux, a run that could not plant the anchor would
	// pass or flake on luck, so it stops instead.
	if _, err := exec.LookPath(tmux.Binary); err == nil {
		if out, err := sharedTmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor session: %v: %s\n", err, out)
			return 1
		}
	}
	// A real banner on macOS builds that helper from this binary.
	postNotification = func(notify.Event) {}
	showCommand = func(string) {}
	code := m.Run()
	sharedTmuxCmd("kill-server").Run()
	return code
}

// socketTmuxCmd builds a raw tmux command aimed at one of the test servers.
func socketTmuxCmd(socket string, args ...string) *exec.Cmd {
	cmd := exec.Command(tmux.Binary, append([]string{"-L", socket}, args...)...)
	if runtime.GOOS == "windows" {
		cmd.Env = append(os.Environ(), "PSMUX_NO_WARM=1")
	}
	return cmd
}

// sharedTmuxCmd aims a raw tmux command at testSocket.
func sharedTmuxCmd(args ...string) *exec.Cmd { return socketTmuxCmd(testSocket, args...) }

// tmuxCmd aims a raw tmux command at the server t's models run on, the
// socket buildModel's driver uses.
func tmuxCmd(t *testing.T, args ...string) *exec.Cmd {
	return socketTmuxCmd(testServer(t), args...)
}

// sessionTmuxCmd aims a server-scoped command (a global option, a binding)
// at the server hosting session id. tmux has one server for all sessions;
// psmux runs one per session and is told which with a leading -t.
func sessionTmuxCmd(t *testing.T, id string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		args = append([]string{"-t", "am_" + id}, args...)
	}
	return tmuxCmd(t, args...)
}

// testServers names the server each top-level test's models run on. On a
// server of its own a test never sees another test's sessions, key bindings
// or global options, and its cleanup takes down that server alone, which is
// what lets tests run in parallel. Subtests share their parent's server.
var testServers = struct {
	sync.Mutex
	byTest map[string]string
	next   int
}{byTest: map[string]string{}}

// testServer returns the socket of t's server, starting the server on first
// use. Names keep one width, so none is a prefix of another or of
// testSocket, and stay short for the socket path limit.
func testServer(t *testing.T) string {
	t.Helper()
	test, _, _ := strings.Cut(t.Name(), "/")
	testServers.Lock()
	socket, ok := testServers.byTest[test]
	if !ok {
		testServers.next++
		socket = fmt.Sprintf("amui%05d", testServers.next)
		testServers.byTest[test] = socket
	}
	testServers.Unlock()
	if ok {
		return socket
	}
	t.Cleanup(func() {
		stopTestServer(socket)
		testServers.Lock()
		delete(testServers.byTest, test)
		testServers.Unlock()
	})
	startTestServer(t, socket)
	return socket
}

// startTestServer plants the anchor that holds a tmux server up for the
// whole test, as TestMain does for testSocket, after clearing whatever an
// interrupted run left on the name. psmux runs a server per session, so
// there is no shared server to hold up.
func startTestServer(t *testing.T, socket string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	// kill-server fails whenever no server is up, which is the normal case.
	socketTmuxCmd(socket, "kill-server").Run()
	if out, err := socketTmuxCmd(socket, "new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
		t.Fatalf("anchor session on %s: %v: %s", socket, err, out)
	}
}

// stopTestServer takes the test's server down once its models have killed
// their sessions; on psmux those sessions were the servers.
func stopTestServer(socket string) {
	if runtime.GOOS == "windows" {
		return
	}
	socketTmuxCmd(socket, "kill-server").Run()
}
