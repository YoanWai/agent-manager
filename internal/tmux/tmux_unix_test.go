//go:build !windows

package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The startup list-keys brings up a server that holds no session for a
// moment, and tmux answers list-panes there with "no current target".
func TestPanesOnAServerWithNoSessionsIsEmpty(t *testing.T) {
	requireTmux(t)
	socket := testSocket + "empty"
	driver, err := NewWithSocket(socket)
	if err != nil {
		t.Fatal(err)
	}
	killServer := func() { exec.Command("tmux", "-L", socket, "kill-server").Run() }
	killServer()
	t.Cleanup(killServer)
	if out, err := exec.Command("tmux", "-L", socket, "start-server", ";", "set-option", "-g", "exit-empty", "off").CombinedOutput(); err != nil {
		t.Fatalf("start an empty server: %v: %s", err, out)
	}
	panes, err := driver.Panes()
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if len(panes) != 0 {
		t.Fatalf("Panes = %v, want none", panes)
	}
}

// A relaunch line typed whole is dropped by a shell without line editing
// once it passes 1024 bytes, and a Codex relaunch with its hooks comes
// close on its own.
func TestRunInPaneDeliversALongCommandToAShellWithoutLineEditing(t *testing.T) {
	driver := requireTmux(t)
	id := "rerun" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "exec /bin/dash", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	marker := filepath.Join(t.TempDir(), "ran")
	payload := strings.Repeat("x", 1500)
	if err := driver.RunInPane(id, map[string]string{"AGENT_MANAGER_SESSION_ID": "abc123"}, "printf '%s %s' \"$AGENT_MANAGER_SESSION_ID\" '"+payload+"' > "+ShellQuote(marker+".part")+" && mv "+ShellQuote(marker+".part")+" "+ShellQuote(marker)); err != nil {
		t.Fatalf("RunInPane: %v", err)
	}
	if got := waitForFile(t, driver, id, marker); got != "abc123 "+payload {
		t.Fatalf("the pane ran %d bytes, want the session env and the %d-byte payload", len(got), len(payload))
	}
}

// A relaunched agent is the pane's foreground command, as a launched one is,
// and its script leaves nothing behind in the shared temp dir to be planted
// or reused.
func TestRunInPaneLeavesTheAgentInFrontAndNoScript(t *testing.T) {
	driver := requireTmux(t)
	id := "front" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "exec /bin/dash", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	if err := driver.RunInPane(id, nil, "sleep 30"); err != nil {
		t.Fatalf("RunInPane: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := tmuxCmd("display-message", "-p", "-t", "am_"+id+":.0", "#{pane_current_command}").Output()
		if err == nil && strings.TrimSpace(string(out)) == "sleep" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane command = %q, want the relaunched agent", strings.TrimSpace(string(out)))
		}
		time.Sleep(50 * time.Millisecond)
	}
	left, err := filepath.Glob(filepath.Join(os.TempDir(), "am-relaunch-"+id+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("relaunch scripts left behind: %v", left)
	}
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

// A terminal pane carries no session id in its environment, so the CLI
// running in one asks which managed session tmux filed that pane under.
func TestSessionOfPaneResolvesAManagedPane(t *testing.T) {
	driver := requireTmux(t)
	id := "pane" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	pane := paneID(t, id)
	got, err := driver.SessionOfPane(tmuxEnv(t, driver), pane)
	if err != nil {
		t.Fatalf("SessionOfPane: %v", err)
	}
	if got != id {
		t.Fatalf("SessionOfPane = %q, want %q", got, id)
	}

	// A $TMUX naming another server belongs to the user's own tmux, whose
	// pane ids mean nothing here.
	got, err = driver.SessionOfPane("/tmp/tmux-999/somebody-else,"+serverPid(t)+",0", pane)
	if err != nil {
		t.Fatalf("SessionOfPane on a foreign socket: %v", err)
	}
	if got != "" {
		t.Fatalf("a foreign socket resolved to %q", got)
	}

	if got, err := driver.SessionOfPane("", pane); err != nil || got != "" {
		t.Fatalf("an empty TMUX resolved to %q, err %v", got, err)
	}
}

// Muse starts an MCP server with none of the pane's environment, so the
// server can only find its session by walking up to the pane's process.
func TestSessionOfProcessWalksUpToAManagedPane(t *testing.T) {
	driver := requireTmux(t)
	id := "proc" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "sleep 60", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	out, err := tmuxCmd("display-message", "-p", "-t", PaneTarget(id), "#{pane_pid}").CombinedOutput()
	if err != nil {
		t.Fatalf("pane pid: %v: %s", err, out)
	}
	panePID := strings.TrimSpace(string(out))
	var child int
	for deadline := time.Now().Add(5 * time.Second); child == 0 && time.Now().Before(deadline); {
		out, _ := exec.Command("pgrep", "-P", panePID, "sleep").Output()
		child, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		time.Sleep(50 * time.Millisecond)
	}
	if child == 0 {
		t.Fatalf("no sleep process under pane pid %s", panePID)
	}

	if got, err := driver.SessionOfProcess(child); err != nil || got != id {
		t.Fatalf("SessionOfProcess(pane child) = %q, %v; want %q", got, err, id)
	}
	if got, err := driver.SessionOfProcess(os.Getpid()); err != nil || got != "" {
		t.Fatalf("SessionOfProcess(test process) = %q, %v; want no session", got, err)
	}
}

// tmux hands the launch to its default-shell with -c, and Debian's dash or
// fish stays on above a command it was not told to exec. A default-shell that
// always waits on its command stands in for them on every platform. The launch
// script has to be the pane's process, so the agent it starts is the pane's
// only child.
func TestCreateRunsTheLaunchScriptAsThePaneProcess(t *testing.T) {
	driver := requireTmux(t)
	waitingShell := t.TempDir() + "/waiting-sh"
	if err := os.WriteFile(waitingShell, []byte("#!/bin/sh\neval \"$2\"\nexit $?\n"), 0o700); err != nil {
		t.Fatalf("waiting shell: %v", err)
	}
	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create("hold"+stamp, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill("hold" + stamp) })
	out, err := tmuxCmd("show-options", "-gv", "default-shell").CombinedOutput()
	if err != nil {
		t.Fatalf("default-shell: %v: %s", err, out)
	}
	previous := strings.TrimSpace(string(out))
	if out, err := tmuxCmd("set-option", "-g", "default-shell", waitingShell).CombinedOutput(); err != nil {
		t.Fatalf("set default-shell: %v: %s", err, out)
	}
	t.Cleanup(func() { tmuxCmd("set-option", "-g", "default-shell", previous).Run() })

	id := "launch" + stamp
	if err := driver.Create(id, "/tmp", "sleep 61", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	panePID, err := driver.PanePID(id)
	if err != nil {
		t.Fatalf("PanePID: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if exec.Command("pgrep", "-P", strconv.Itoa(panePID), "sleep").Run() == nil {
			return
		}
	}
	tree, _ := exec.Command("ps", "-A", "-o", "pid=,ppid=,args=").Output()
	t.Fatalf("no sleep directly under pane pid %d:\n%s", panePID, tree)
}

// An agent asks for extended keys as it starts, so they must be on before it runs.
func TestCreateDeliversShiftEnterToAnAgentThatAsksForExtendedKeys(t *testing.T) {
	driver := requireTmux(t)
	requireTmuxAtLeast(t, 3, 2)
	id := "xkeys" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	received := filepath.Join(t.TempDir(), "keys")
	if err := driver.Create(id, "/tmp", `printf '\033[>4;2m'; exec head -n 1 > `+ShellQuote(received), nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	waitForForegroundCommand(t, id, "head")
	if out, err := tmuxCmd("send-keys", "-t", "am_"+id, "S-Enter", "Enter").CombinedOutput(); err != nil {
		t.Fatalf("send-keys: %v: %s", err, out)
	}
	if got := strings.TrimSpace(waitForFile(t, driver, id, received)); got != "\x1b[13;2u" {
		t.Fatalf("Shift+Enter reached the agent as %q, want CSI 13;2u", got)
	}
}

// A session outside the am_ namespace is one the user started on this
// server themselves, not a managed session the CLI may act as.
func TestSessionOfPaneIgnoresAnUnmanagedSession(t *testing.T) {
	driver := requireTmux(t)
	name := "unmanaged" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if out, err := tmuxCmd("new-session", "-d", "-s", name).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v: %s", err, out)
	}
	t.Cleanup(func() { tmuxCmd("kill-session", "-t", name).Run() })

	out, err := tmuxCmd("display-message", "-p", "-t", name, "#{pane_id}").CombinedOutput()
	if err != nil {
		t.Fatalf("pane id: %v: %s", err, out)
	}
	pane := strings.TrimSpace(string(out))
	got, err := driver.SessionOfPane(tmuxEnv(t, driver), pane)
	if err != nil {
		t.Fatalf("SessionOfPane: %v", err)
	}
	if got != "" {
		t.Fatalf("an unmanaged session resolved to %q", got)
	}
}

// tmux reports the socket with its symlinks resolved, and a $TMUX copied
// out of a pane can name the same file through a symlinked directory: on
// macOS /tmp is itself a link to /private/tmp. The two still have to meet.
func TestSessionOfPaneMatchesASymlinkedSocketPath(t *testing.T) {
	driver := requireTmux(t)
	id := "link" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	socket := driver.SocketPath()
	link := filepath.Join(t.TempDir(), "socketdir")
	if err := os.Symlink(filepath.Dir(socket), link); err != nil {
		t.Fatalf("symlink the socket directory: %v", err)
	}
	through := filepath.Join(link, filepath.Base(socket))
	got, err := driver.SessionOfPane(through+","+serverPid(t)+",0", paneID(t, id))
	if err != nil {
		t.Fatalf("SessionOfPane: %v", err)
	}
	if got != id {
		t.Fatalf("SessionOfPane through a symlink = %q, want %q", got, id)
	}
}

// tmux reads a target it cannot parse as the current session and exits 0,
// so anything that is not a pane id has to answer empty before tmux sees it.
func TestSessionOfPaneRejectsAMalformedPaneID(t *testing.T) {
	driver := requireTmux(t)
	id := "bad" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	for _, pane := range []string{"", "-X", ".", ":", "%", "%1x", "am_" + id} {
		got, err := driver.SessionOfPane(tmuxEnv(t, driver), pane)
		if err != nil || got != "" {
			t.Fatalf("pane %q resolved to %q, err %v", pane, got, err)
		}
	}
}

// A restarted server keeps its socket path but numbers panes from %0 again,
// so a $TMUX left over from the previous server must not name a session on
// this one.
func TestSessionOfPaneIgnoresAnotherServerOnTheSameSocket(t *testing.T) {
	driver := requireTmux(t)
	id := "pid" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	pid, err := strconv.Atoi(serverPid(t))
	if err != nil {
		t.Fatalf("server pid: %v", err)
	}
	stale := driver.SocketPath() + "," + strconv.Itoa(pid+1) + ",0"
	got, err := driver.SessionOfPane(stale, paneID(t, id))
	if err != nil {
		t.Fatalf("SessionOfPane: %v", err)
	}
	if got != "" {
		t.Fatalf("a $TMUX from another server resolved to %q", got)
	}
}

func tmuxEnv(t *testing.T, driver *Driver) string {
	t.Helper()
	return driver.SocketPath() + "," + serverPid(t) + ",0"
}

func paneID(t *testing.T, id string) string {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "-t", PaneTarget(id), "#{pane_id}").CombinedOutput()
	if err != nil {
		t.Fatalf("pane id for %s: %v: %s", id, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Ctrl-C reaches the agent alone, and the script must outlive it to print
// the hint. dash and Linux bash exit when a foreground child dies of
// SIGINT, so on Linux the pane closed instead.
func TestLaunchScriptNamesTheWayBackWhenCtrlCKillsTheAgent(t *testing.T) {
	driver := requireTmux(t)
	for _, shell := range []string{"dash", "bash"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skip(shell + " not installed")
			}
			id := "sigint" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
			path, err := writeLaunchScript(id, nil, "sleep 1000", "")
			if err != nil {
				t.Fatalf("writeLaunchScript: %v", err)
			}
			t.Cleanup(func() { os.Remove(path) })
			if out, err := tmuxCmd("new-session", "-d", "-s", "am_"+id, shell+" "+ShellQuote(path)).CombinedOutput(); err != nil {
				t.Fatalf("new-session: %v: %s", err, out)
			}
			t.Cleanup(func() { driver.Kill(id) })
			waitForForegroundCommand(t, id, "sleep")
			if out, err := tmuxCmd("send-keys", "-t", "am_"+id, "C-c").CombinedOutput(); err != nil {
				t.Fatalf("send-keys: %v: %s", err, out)
			}
			waitForPane(t, driver, id, relaunchHint)
		})
	}
}

func waitForForegroundCommand(t *testing.T, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		out, _ := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{pane_current_command}").Output()
		got = strings.TrimSpace(string(out))
		if got == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane foreground command = %q, want %q", got, want)
}
