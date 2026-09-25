package tmux

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	driver := requireTmux(t)
	id := "test" + time.Now().Format("150405.000000")
	id = strings.ReplaceAll(id, ".", "")

	if err := driver.Create(id, "/tmp", "printf 'hello-pane-marker'", nil, 0, 0); err != nil {
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

// A window an agent split shares its geometry with the teammate panes, so
// pinning the window alone leaves the agent's own pane -- the one the
// preview draws -- at a fraction of the panel it is drawn in. Either split
// axis takes that room, and a preview box that grew or shrank has to leave
// the teammate its share of the axis the split divides, since that is the
// room the fit could otherwise take: a pane that loses width reflows every
// line it holds, and one that loses height clears a Codex scrollback
// (#369). The other axis is the window's own and every pane follows it.
func TestResizeFitsTheAgentPaneInASplitWindow(t *testing.T) {
	for _, split := range []struct {
		axis string
		flag string
		// divided indexes the dimension the split cuts, the one the panes
		// share out between them rather than take whole from the window.
		divided int
	}{{"horizontal", "-h", 0}, {"vertical", "-v", 1}} {
		for _, box := range []struct {
			name          string
			width, height int
		}{{"grown", 100, 30}, {"shrunk", 60, 20}} {
			t.Run(split.axis+"/"+box.name, func(t *testing.T) {
				driver := requireTmux(t)
				id := "fit" + split.axis[:1] + box.name[:1] + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
				if err := driver.Create(id, "/tmp", "cat", nil, 80, 24); err != nil {
					t.Fatalf("Create: %v", err)
				}
				t.Cleanup(func() { driver.Kill(id) })
				if out, err := tmuxCmd("split-window", split.flag, "-t", "am_"+id, "--", "sh", "-c", "sleep 30").CombinedOutput(); err != nil {
					t.Fatalf("split-window: %v: %s", err, out)
				}
				teammate := teammateSize(t, id)

				if err := driver.Resize(id, box.width, box.height); err != nil {
					t.Fatalf("Resize: %v", err)
				}

				panes, err := driver.Panes()
				if err != nil {
					t.Fatalf("Panes: %v", err)
				}
				if got := panes[id]; got.Width != box.width || got.Height != box.height {
					t.Fatalf("agent pane = %dx%d, want the preview box %dx%d", got.Width, got.Height, box.width, box.height)
				}
				if after := teammateSize(t, id); after[split.divided] < teammate[split.divided] {
					t.Fatalf("teammate pane = %v, want no less than the %v it had across the split", after, teammate)
				}
			})
		}
	}
}

// A geometry line tmux did not answer in numbers leaves the agent pane at
// whatever the split gave it, so the resize reports rather than returns.
func TestResizeReportsUnreadableGeometry(t *testing.T) {
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *display-message*) echo 'no geometry here';; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	err := driver.Resize("x1", 100, 30)

	if err == nil || !strings.Contains(err.Error(), "geometry") {
		t.Fatalf("Resize error = %v, want the unreadable geometry reported", err)
	}
}

// A window someone opened inside a session becomes that session's current
// window, which is not the one the agent runs in. Everything the preview
// pins has to stay on the agent's window through that.
func TestResizePinsTheAgentWindowNotTheCurrentOne(t *testing.T) {
	driver := requireTmux(t)
	id := "window" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "cat", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	// No -d: the new window is left current, which is what a session-wide
	// target would resize.
	if out, err := tmuxCmd("new-window", "-t", "am_"+id, "--", "sh", "-c", "sleep 30").CombinedOutput(); err != nil {
		t.Fatalf("new-window: %v: %s", err, out)
	}

	if err := driver.Resize(id, 100, 30); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	panes, err := driver.Panes()
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if got := panes[id]; got.Width != 100 || got.Height != 30 {
		t.Fatalf("agent pane = %dx%d, want the preview box 100x30", got.Width, got.Height)
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
