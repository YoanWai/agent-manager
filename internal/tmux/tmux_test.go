package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amtmuxtest"

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
	// kill-server fails whenever no server is up, which is the normal case.
	tmuxCmd("kill-server").Run()
	// Without tmux the run still starts: each test skips through its own
	// requireTmux. With tmux, a run that could not plant the anchor would
	// pass or flake on luck, so it stops instead.
	if _, err := exec.LookPath("tmux"); err == nil {
		if out, err := tmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor session: %v: %s\n", err, out)
			os.Exit(1)
		}
	}
	code := m.Run()
	tmuxCmd("kill-server").Run()
	os.Exit(code)
}

// tmuxCmd builds a raw tmux command aimed at the test socket.
func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", testSocket}, args...)...)
}

func requireTmux(t *testing.T) *Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return driver
}

func windowSizeOption(t *testing.T, id string) string {
	t.Helper()
	out, err := tmuxCmd("show-window-options", "-v", "-t", "am_"+id, "window-size").CombinedOutput()
	if err != nil {
		t.Fatalf("show-window-options: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// pasteReady follows the paste-mode request, so a pane showing it has bracketed paste on.
const pasteReady = "paste-ready"

// clearPaneTheme drops the server-global colors a pane-theme test left
// behind, so the rest of the package sees an unstyled server.
func clearPaneTheme(t *testing.T) {
	t.Helper()
	tmuxCmd("set-option", "-gu", "window-style").Run()
	tmuxCmd("set-environment", "-gu", "COLORFGBG").Run()
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

func globalWindowStyle(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("show-options", "-gv", "window-style").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options window-style: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
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
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
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

func teammateSize(t *testing.T, id string) [2]int {
	t.Helper()
	out, err := tmuxCmd("list-panes", "-t", "am_"+id, "-f", "#{==:#{pane_index},1}", "-F", "#{pane_width} #{pane_height}").CombinedOutput()
	if err != nil {
		t.Fatalf("list-panes: %v: %s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("teammate pane geometry = %q", out)
	}
	width, _ := strconv.Atoi(fields[0])
	height, _ := strconv.Atoi(fields[1])
	return [2]int{width, height}
}

func TestSocketPathNamesTheRunningServer(t *testing.T) {
	driver := requireTmux(t)
	out, err := tmuxCmd("display-message", "-p", "#{socket_path}").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := strings.TrimSpace(string(out))
	if got := driver.SocketPath(); got != want {
		t.Fatalf("SocketPath = %q, want tmux's own %q", got, want)
	}
}

// The -L name is shared by every manager; only the path tells two servers
// apart, and it is the path a session is stamped with.
func TestSocketPathSeparatesServersUnderOneName(t *testing.T) {
	here := requireTmux(t)
	herePath := here.SocketPath()
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	elsewhere, err := NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	if herePath == elsewhere.SocketPath() {
		t.Fatalf("both servers resolved to %q", herePath)
	}
	if !strings.HasSuffix(elsewhere.SocketPath(), "/"+testSocket) {
		t.Fatalf("path %q does not end in the socket name", elsewhere.SocketPath())
	}
}

// tmux resolves a relative TMUX_TMPDIR from its own working directory, so
// the path a session is stamped with has to be the absolute one or a later
// poll reads its own sessions as another server's.
func TestSocketPathFromRelativeTmpdirIsAbsolute(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll("sockets", 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", "sockets")

	got := socketPathFromEnv(testSocket)
	if !filepath.IsAbs(got) {
		t.Fatalf("socket path %q is not absolute", got)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, "sockets"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(resolved, fmt.Sprintf("tmux-%d", os.Getuid()), testSocket)
	if got != want {
		t.Fatalf("socket path = %q, want %q", got, want)
	}
}

// tmux skips a TMUX_TMPDIR it cannot resolve and puts its socket under /tmp.
func TestSocketPathFromMissingTmpdirFallsBackToTmp(t *testing.T) {
	t.Setenv("TMUX_TMPDIR", filepath.Join(t.TempDir(), "missing"))
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, fmt.Sprintf("tmux-%d", os.Getuid()), testSocket)
	if got := socketPathFromEnv(testSocket); got != want {
		t.Fatalf("socket path = %q, want %q", got, want)
	}
}

func sessionOf(t *testing.T, detach, review, editor []string) keybind.Table {
	t.Helper()
	return keybind.DefaultSession().
		With(keybind.Detach, bindingOf(t, detach...)).
		With(keybind.Review, bindingOf(t, review...)).
		With(keybind.Editor, bindingOf(t, editor...))
}

func bindingOf(t *testing.T, specs ...string) keybind.Binding {
	t.Helper()
	keys := make([]keybind.Key, 0, len(specs))
	for _, spec := range specs {
		key, err := keybind.Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}

// ownedRootLines reads the root-table bindings the manager owns, keyed by
// the key name as unbind-key takes it.
func ownedRootLines(t *testing.T) map[string]string {
	t.Helper()
	bound, err := tmuxCmd("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	owned := map[string]string{}
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(line, ownedBindingTest) {
			continue
		}
		owned[strings.ReplaceAll(fields[3], `\\`, `\`)] = line
	}
	return owned
}

func restoreDefaultKeys(t *testing.T, driver *Driver) {
	t.Helper()
	t.Cleanup(func() {
		driver.SetSessionKeys(keybind.DefaultSession())
		if err := driver.EnsureBindings(); err != nil {
			t.Errorf("restore default bindings: %v", err)
		}
	})
}

func tmuxEnv(t *testing.T, driver *Driver) string {
	t.Helper()
	return driver.SocketPath() + "," + serverPid(t) + ",0"
}

func serverPid(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "#{pid}").CombinedOutput()
	if err != nil {
		t.Fatalf("server pid: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func paneID(t *testing.T, id string) string {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "-t", PaneTarget(id), "#{pane_id}").CombinedOutput()
	if err != nil {
		t.Fatalf("pane id for %s: %v: %s", id, err, out)
	}
	return strings.TrimSpace(string(out))
}
