package tmux

import (
	"github.com/YoanWai/agent-manager/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An agent that changes directory mid-session, as Claude Code's /cd and
// EnterWorktree do, is a child of the launch script, and tmux reports the
// directory of the pane's foreground process group.
func TestPanesFollowsTheAgentIntoANewDirectory(t *testing.T) {
	testenv.SkipPOSIXShell(t)
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

// An agent is free to split its own window and leave the new pane focused,
// which Claude Code's agent teams do when they run a teammate beside their
// leader. Everything the manager reads and types has to stay on the agent's
// own pane through that.
func TestManagerStaysOnTheAgentPaneAfterASplit(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	driver := requireTmux(t)
	id := "split" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "cat", nil, 0, 0); err != nil {
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
	testenv.SkipPOSIXShell(t)
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
