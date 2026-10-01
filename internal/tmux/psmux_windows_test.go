//go:build windows

package tmux

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/pwsh"
)

// psmuxSession creates a managed session running command and waits for its
// pane to show want.
func psmuxSession(t *testing.T, driver *Driver, prefix, command, want string) string {
	t.Helper()
	id := prefix + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), command, nil, 100, 30); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	if want != "" {
		waitForPaneLine(t, driver, id, want)
	}
	return id
}

// waitForPaneLine waits for a pane line that is exactly want, so a typed
// command echoing its own text does not count as its output.
func waitForPaneLine(t *testing.T, driver *Driver, id, want string) {
	t.Helper()
	var pane string
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		var err error
		if pane, err = driver.CapturePane(id); err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		for _, line := range strings.Split(stripANSI(pane), "\n") {
			if strings.TrimSpace(line) == want {
				return
			}
		}
	}
	t.Fatalf("pane never showed %q:\n%s", want, pane)
}

func stripANSI(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			out.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
		}
	}
	return out.String()
}

// The launch script runs under PowerShell in the session's first pane, and
// the pane listing finds it without list-panes -f.
func TestPsmuxCreateRunsTheLaunchScript(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "ready", "Write-Output agent-ready", "agent-ready")
	panes, err := driver.Panes()
	if err != nil {
		t.Fatalf("Panes: %v", err)
	}
	if panes[id].PID <= 0 {
		t.Fatalf("Panes should map %q to a pane pid, got %v", id, panes)
	}
}

// A message typed into the pane runs in the PowerShell the script left at
// a prompt.
func TestPsmuxSendTextReachesThePaneShell(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "type", "Write-Output launched", "launched")
	if err := driver.SendText(id, "Write-Output ('x'+'y')"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitForPaneLine(t, driver, id, "xy")
}

// Bindings live on each session's own server, and Create puts them there:
// detach straight to detach-client, requests through a run-shell that sets
// the marker and detaches, and the keys recorded for the next run.
func TestPsmuxCreateBindsTheSessionServer(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "keys", "", "")
	server := func(args ...string) string {
		t.Helper()
		out, err := tmuxCmd(append(sessionRoute(id), args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
		return string(out)
	}
	root := server("list-keys", "-T", "root")
	lines := map[string]string{}
	for _, line := range strings.Split(root, "\n") {
		if fields := strings.Fields(line); len(fields) >= 4 {
			lines[fields[3]] = line
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(lines["C-q"]), " detach-client") {
		t.Fatalf("C-q should detach; root table:\n%s", root)
	}
	if !strings.Contains(lines["C-r"], "run-shell") || !strings.Contains(lines["F3"], "run-shell") {
		t.Fatalf("C-r and F3 should run the request script; root table:\n%s", root)
	}
	recorded := strings.Fields(server("show-option", "-gqv", rootKeysOption))
	if !slices.Contains(recorded, "C-q") || !slices.Contains(recorded, "C-r") || !slices.Contains(recorded, "F3") {
		t.Fatalf("recorded root keys = %q", recorded)
	}
}

// The script a request key runs leaves the marker on the session's server
// through PowerShell, the shell psmux's run-shell uses.
func TestPsmuxRequestScriptLeavesTheMarker(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "script", "", "")
	action := driver.requestAction(id, RequestEditor)
	shell, err := pwsh.Path()
	if err != nil {
		t.Skip(err)
	}
	if out, err := exec.Command(shell, "-NoProfile", "-Command", action[1]).CombinedOutput(); err != nil {
		t.Fatalf("request script: %v: %s", err, out)
	}
	if got, err := driver.PendingRequest(id); err != nil || got != RequestEditor {
		t.Fatalf("PendingRequest = %q, %v; want %q", got, err, RequestEditor)
	}
}

// The detach marker is read and cleared on the server of the session the
// user detached from.
func TestPsmuxRequestMarkerIsPerSession(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "req", "", "")
	if out, err := tmuxCmd(append(sessionRoute(id), "set-option", "-g", requestOption, RequestReview)...).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v: %s", err, out)
	}
	if got, err := driver.PendingRequest(id); err != nil || got != RequestReview {
		t.Fatalf("PendingRequest = %q, %v; want %q", got, err, RequestReview)
	}
	if err := driver.ClearRequest(id); err != nil {
		t.Fatalf("ClearRequest: %v", err)
	}
	if got, err := driver.PendingRequest(id); err != nil || got != "" {
		t.Fatalf("PendingRequest after clear = %q, %v; want empty", got, err)
	}
}

// The manager stays off psmux's control client, whose output thread panics
// on wide lines; the protocol itself still parses, which a reply short of
// that limit shows.
func TestPsmuxControlModeAnswersCommands(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "ctl", "", "")
	if _, err := driver.OpenControl(id); err == nil {
		t.Fatal("OpenControl should refuse psmux's control client")
	}
	control, err := driver.openControl(id)
	if err != nil {
		t.Fatalf("openControl: %v", err)
	}
	defer control.Close()
	got, err := control.Command("display-message -p '#{session_name}'")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if strings.TrimSpace(got) != "am_"+id {
		t.Fatalf("session name over control = %q, want am_%s", got, id)
	}
}

// A pane's $TMUX names its server's pid; that and the pane id pick the
// session, and a pid from any other server answers empty.
func TestPsmuxSessionOfPaneUsesTheServerPid(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "pane", "", "")
	out, err := tmuxCmd("display-message", "-p", "-t", PaneTarget(id), "#{pid} #{pane_id}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v: %s", err, out)
	}
	pid, pane, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	env := "/tmp/psmux-" + pid + "/" + testSocket + ",1,0"
	if got, err := driver.SessionOfPane(env, pane); err != nil || got != id {
		t.Fatalf("SessionOfPane = %q, %v; want %q", got, err, id)
	}
	if got, err := driver.SessionOfPane("/tmp/psmux-1"+pid+"/"+testSocket+",1,0", pane); err != nil || got != "" {
		t.Fatalf("a stale server pid resolved to %q, %v", got, err)
	}
	if got, err := driver.SessionOfPane("/tmp/psmux-"+pid+"/elsewhere,1,0", pane); err != nil || got != "" {
		t.Fatalf("another namespace resolved to %q, %v", got, err)
	}
}

// Kill takes the session's server down and its launch script with it.
func TestPsmuxKillRemovesSessionAndScript(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "kill", "Write-Output started", "started")
	if _, err := os.Stat(launchScriptPath(id)); err != nil {
		t.Fatalf("launch script: %v", err)
	}
	if err := driver.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if driver.Exists(id) {
		t.Fatal("session should be gone after Kill")
	}
	if _, err := os.Stat(launchScriptPath(id)); !os.IsNotExist(err) {
		t.Fatalf("launch script should be removed, stat err = %v", err)
	}
}
