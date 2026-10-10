package tmux

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func windowSizeOption(t *testing.T, id string) string {
	t.Helper()
	out, err := tmuxCmd("show-window-options", "-v", "-t", "am_"+id, "window-size").CombinedOutput()
	if err != nil {
		t.Fatalf("show-window-options: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// Resize pins the detached window to a manual size for the preview, and
// PrepareAttach flips it back to auto so the attaching client fills it
// instead of leaving tmux's dotted out-of-bounds overlay on the right.
func TestPrepareAttachRestoresAutoSize(t *testing.T) {
	driver := requireTmux(t)
	id := "attach" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 100, 30); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if err := driver.Resize(id, 80, 24); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if got := windowSizeOption(t, id); got != "manual" {
		t.Fatalf("after Resize, window-size = %q, want manual", got)
	}

	if err := driver.PrepareAttach(id); err != nil {
		t.Fatalf("PrepareAttach: %v", err)
	}
	want := "latest"
	if driver.attachSizeLargest.Load() {
		want = "largest"
	}
	if got := windowSizeOption(t, id); got != want {
		t.Fatalf("after PrepareAttach, window-size = %q, want %q", got, want)
	}
}

// A pre-3.1 server rejects window-size "latest" with "unknown value";
// PrepareAttach must retry with "largest" and remember the verdict. A stub
// tmux that rejects "latest" stands in for the old server and logs its
// calls, so the test also proves the second attach skips the doomed try.
func TestPrepareAttachFallsBackWhenLatestRejected(t *testing.T) {
	dir := t.TempDir()
	callLog := dir + "/calls"
	stub := dir + "/tmux"
	script := "#!/bin/sh\necho \"$@\" >> " + callLog + "\ncase \"$*\" in *latest*) echo 'unknown value: latest' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if err := driver.PrepareAttach("x1"); err != nil {
		t.Fatalf("PrepareAttach with rejecting server: %v", err)
	}
	if err := driver.PrepareAttach("x1"); err != nil {
		t.Fatalf("second PrepareAttach: %v", err)
	}
	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	calls := strings.Split(strings.TrimSpace(string(logged)), "\n")
	if len(calls) != 3 {
		t.Fatalf("got %d tmux calls, want 3 (latest, largest, largest):\n%s", len(calls), logged)
	}
	for i, wantValue := range []string{"latest", "largest", "largest"} {
		if !strings.HasSuffix(calls[i], "window-size "+wantValue) {
			t.Fatalf("call %d = %q, want window-size %s", i, calls[i], wantValue)
		}
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
