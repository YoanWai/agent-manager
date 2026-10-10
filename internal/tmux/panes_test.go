package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// An agent that changes directory mid-session, as Claude Code's /cd and
// EnterWorktree do, is a child of the launch script, and tmux reports the
// directory of the pane's foreground process group.
func TestPanesFollowsTheAgentIntoANewDirectory(t *testing.T) {
	driver := requireTmux(t)
	id := "cwd" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	moved := filepath.Join(t.TempDir(), "moved dir")
	if err := os.Mkdir(moved, 0o755); err != nil {
		t.Fatal(err)
	}
	moved, err := filepath.EvalSymlinks(moved)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Create(id, "/tmp", "sh -c "+ShellQuote("cd "+ShellQuote(moved)+" && exec sleep 30"), nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	var got string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		panes, err := driver.Panes()
		if err != nil {
			t.Fatalf("Panes: %v", err)
		}
		if got = panes[id].Path; got == moved {
			// the tty rides just ahead of the path, which holds a space
			if tty := panes[id].TTY; !strings.HasPrefix(tty, "/dev/") {
				t.Fatalf("Panes tty = %q, want a /dev/ device", tty)
			}
			return
		}
	}
	t.Fatalf("Panes path = %q, want %q", got, moved)
}

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

// An agent is free to split its own window and leave the new pane focused,
// which Claude Code's agent teams do when they run a teammate beside their
// leader. Everything the manager reads and types has to stay on the agent's
// own pane through that.
func TestManagerStaysOnTheAgentPaneAfterASplit(t *testing.T) {
	driver := requireTmux(t)
	id := "split" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "cat", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	agentPID, err := driver.PanePID(id)
	if err != nil {
		t.Fatalf("PanePID: %v", err)
	}

	// No -d: the split leaves the teammate pane active, which is what a
	// session-wide target would follow. Its own directory is what tells the
	// two panes apart in what the manager reads back.
	teammateDir := t.TempDir()
	if out, err := tmuxCmd("split-window", "-t", "am_"+id, "-c", teammateDir, "--",
		"sh", "-c", "printf teammate-pane; sleep 30").CombinedOutput(); err != nil {
		t.Fatalf("split-window: %v: %s", err, out)
	}

	if pid, err := driver.PanePID(id); err != nil || pid != agentPID {
		t.Fatalf("PanePID after split = %d, %v, want the agent pane %d", pid, err, agentPID)
	}
	panes, err := driver.Panes()
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if panes[id].PID != agentPID {
		t.Fatalf("Panes reports pid %d, want the agent pane %d", panes[id].PID, agentPID)
	}
	if path, err := driver.PaneCurrentPath(id); err != nil || path == teammateDir {
		t.Fatalf("PaneCurrentPath = %q, %v, want the agent pane's own directory", path, err)
	}
	if err := driver.SendText(id, "hello world"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		pane, err = driver.CapturePane(id)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		if strings.Contains(pane, "hello world") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(pane, "hello world") {
		t.Fatalf("message should reach the agent pane, pane: %q", pane)
	}
	if strings.Contains(pane, "teammate-pane") {
		t.Fatalf("capture should read the agent pane, not the teammate: %q", pane)
	}
}

// History captures feed quote recovery, so they must read the agent's own
// pane: a bare session target resolves to whichever pane is active, and an
// agent that split its window would have its quotes read off the teammate.
func TestCapturePaneHistoryTargetsTheAgentPane(t *testing.T) {
	dir := t.TempDir()
	callLog := dir + "/calls"
	stub := dir + "/tmux"
	script := "#!/bin/sh\necho \"$@\" >> " + callLog + "\necho pane\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}
	if _, err := driver.CapturePaneHistory("x1", 300); err != nil {
		t.Fatalf("CapturePaneHistory: %v", err)
	}
	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	if !strings.Contains(string(logged), "-S -300 -t "+PaneTarget("x1")) {
		t.Fatalf("history capture went to the wrong target, calls:\n%s", logged)
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
