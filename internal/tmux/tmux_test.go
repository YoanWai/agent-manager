package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/testenv"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amtmuxtest"

// argsFileEnv makes a rerun of this test binary write the arguments it was
// given to the named file, one per line, and exit: a stand-in agent CLI.
const argsFileEnv = "AM_TMUX_TEST_ARGS_FILE"

// TestMain kills any leftover test server so each run starts and ends clean.
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it
// ("server exited unexpectedly").
func TestMain(m *testing.M) {
	// startOtherProcess runs this binary again as a second agent-manager
	// process, with the tmux binary and the session as its arguments.
	if action := os.Getenv(otherProcessEnv); action != "" {
		os.Exit(repeatUntilStdinCloses(&Driver{bin: os.Args[1], socket: testSocket}, action, os.Args[2]))
	}
	if file := os.Getenv(argsFileEnv); file != "" {
		if err := os.WriteFile(file, []byte(strings.Join(os.Args[1:], "\n")), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	// psmux keeps its servers' registry under its data directory, which a
	// fresh one isolates the way the private socket does for tmux.
	if runtime.GOOS == "windows" {
		dir, err := os.MkdirTemp("", "amtmuxtest-psmux-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "psmux data dir: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		os.Setenv("PSMUX_DATA_DIR", dir)
	}
	// kill-server fails whenever no server is up, which is the normal case.
	tmuxCmd("kill-server").Run()
	// Without tmux the run still starts: each test skips through its own
	// requireTmux. With tmux, a run that could not plant the anchor would
	// pass or flake on luck, so it stops instead.
	if _, err := exec.LookPath(Binary); err == nil {
		if out, err := tmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor session: %v: %s\n", err, out)
			return 1
		}
	}
	code := m.Run()
	tmuxCmd("kill-server").Run()
	return code
}

// tmuxCmd builds a raw tmux command aimed at the test socket.
func tmuxCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(Binary, append([]string{"-L", testSocket}, args...)...)
	cmd.Env = commandEnv()
	return cmd
}

func requireTmux(t *testing.T) *Driver {
	t.Helper()
	if _, err := exec.LookPath(Binary); err != nil {
		t.Skip(Binary + " not installed")
	}
	driver, err := NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return driver
}

func TestDetachRequestRoundTrip(t *testing.T) {
	driver := requireTmux(t)
	// A live server is needed for global options to stick.
	id := "rev" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	t.Cleanup(func() {
		if err := driver.ClearRequest(id); err != nil {
			t.Errorf("ClearRequest: %v", err)
		}
	})

	if err := driver.ClearRequest(id); err != nil {
		t.Fatalf("ClearRequest: %v", err)
	}
	request, err := driver.PendingRequest(id)
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("no request expected on a clean marker, got %q", request)
	}

	for _, want := range []string{RequestReview, RequestEditor} {
		if _, err := tmuxCmd(append(sessionRoute(id), "set-option", "-g", requestOption, want)...).CombinedOutput(); err != nil {
			t.Fatalf("set marker: %v", err)
		}
		request, err = driver.PendingRequest(id)
		if err != nil {
			t.Fatalf("PendingRequest: %v", err)
		}
		if request != want {
			t.Fatalf("marker reads as %q, want %q", request, want)
		}

		if err := driver.ClearRequest(id); err != nil {
			t.Fatalf("ClearRequest: %v", err)
		}
		request, err = driver.PendingRequest(id)
		if err != nil {
			t.Fatalf("PendingRequest: %v", err)
		}
		if request != "" {
			t.Fatalf("clear should drop the marker, got %q", request)
		}
	}
}

func waitForFile(t *testing.T, driver *Driver, id, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return string(data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane, _ := driver.CapturePane(id)
	t.Fatalf("pane never wrote %s; pane:\n%s", path, pane)
	return ""
}

func TestLifecycle(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	driver := requireTmux(t)
	id := "test" + time.Now().Format("150405.000000")
	id = strings.ReplaceAll(id, ".", "")

	if err := driver.Create(id, paneDir(), "printf 'hello-pane-marker'", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if !driver.Exists(id) {
		t.Fatal("session should exist after Create")
	}

	pid, err := driver.PanePID(id)
	if err != nil || pid <= 0 {
		t.Fatalf("PanePID: pid=%d err=%v", pid, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		pane, err = driver.CapturePane(id)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		if strings.Contains(pane, "hello-pane-marker") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(pane, "hello-pane-marker") {
		t.Fatalf("captured pane missing marker: %q", pane)
	}

	panes, err := driver.Panes()
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if panes[id].PID <= 0 {
		t.Fatalf("Panes should map %q to a pane pid, got %v", id, panes)
	}

	if err := driver.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if driver.Exists(id) {
		t.Fatal("session should be gone after Kill")
	}
	if err := driver.Kill(id); err != nil {
		t.Fatalf("Kill on missing session should be a no-op, got %v", err)
	}
}

func TestCommandListSeparatesCommands(t *testing.T) {
	if got := commandList(); got != nil {
		t.Fatalf("no commands = %q, want nil", got)
	}
	if got := commandList([]string{"set-option", "@one", "alpha"}); !slices.Equal(got, []string{"set-option", "@one", "alpha"}) {
		t.Fatalf("one command should reach tmux unchanged, got %q", got)
	}
	got := commandList(
		[]string{"set-option", "@one", "alpha"},
		[]string{"set-option", "@two", "beta"},
		[]string{"set-option", "@three", "gamma"},
	)
	want := []string{
		"set-option", "@one", "alpha", ";",
		"set-option", "@two", "beta", ";",
		"set-option", "@three", "gamma",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("commands should be separated by a lone %q, got %q", ";", got)
	}
}

// The separator is what tmux itself reads, so the encoding has to hold against
// a real server: a value carrying its own semicolon keeps it, and a command
// that fails takes the rest of the list down with it.
func TestCommandListRunsAgainstTmux(t *testing.T) {
	driver := requireTmux(t)
	id := "cmdlist" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	name := "am_" + id

	if _, err := driver.run(commandList(
		[]string{"set-option", "-t", name, "@first", `alpha\;`},
		[]string{"set-option", "-t", name, "@second", "beta"},
	)...); err != nil {
		t.Fatalf("run command list: %v", err)
	}
	if got := sessionOption(t, name, "@first"); got != "alpha;" {
		t.Fatalf("@first = %q, want the escaped semicolon kept", got)
	}
	if got := sessionOption(t, name, "@second"); got != "beta" {
		t.Fatalf("@second = %q, want the command after the separator to run", got)
	}

	// psmux answers an option set on a missing session with a warning at
	// exit 0, so there is no failure for the list to stop at.
	if runtime.GOOS == "windows" {
		return
	}
	if _, err := driver.run(commandList(
		[]string{"set-option", "-t", "am_no_such_session", "@third", "gamma"},
		[]string{"set-option", "-t", name, "@fourth", "delta"},
	)...); err == nil {
		t.Fatal("a list whose first command fails should report the failure")
	}
	if got := sessionOption(t, name, "@fourth"); got != "" {
		t.Fatalf("@fourth = %q, want the command after a failure skipped", got)
	}
}

func sessionOption(t *testing.T, name, option string) string {
	t.Helper()
	out, err := tmuxCmd("show-options", "-v", "-t", name, option).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func waitForPane(t *testing.T, driver *Driver, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		pane, _ = driver.CapturePane(id)
		if strings.Contains(pane, want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane never showed %q; pane:\n%s", want, pane)
}

func serverPid(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "#{pid}").CombinedOutput()
	if err != nil {
		t.Fatalf("server pid: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// paneDir is a directory every platform's sessions can start in.
func paneDir() string {
	if runtime.GOOS == "windows" {
		return os.TempDir()
	}
	return "/tmp"
}

// bindingServer runs commands where a test inspects server-scoped setup.
// tmux keeps it on its one server, which the anchor session holds up; psmux
// keeps it per session server, so there a managed session is created and
// every command is routed to its server.
func bindingServer(t *testing.T, driver *Driver) func(args ...string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS != "windows" {
		return tmuxCmd
	}
	id := "bind" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	return func(args ...string) *exec.Cmd {
		return tmuxCmd(append(sessionRoute(id), args...)...)
	}
}
