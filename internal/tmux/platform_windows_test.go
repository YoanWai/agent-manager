//go:build windows

package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
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
	id := prefixedID(prefix)
	if err := driver.Create(id, paneDir(), command, nil, 100, 30); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	if want != "" {
		waitForPaneLine(t, driver, id, want)
	}
	return id
}

// prefixedID names a test session: prefix plus the time, unique per run.
func prefixedID(prefix string) string {
	return prefix + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
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
	recorded := strings.Fields(server("show-options", "-gqv", rootKeysOption))
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

// A relaunch line past send-keys' 1024 bytes reaches the pane's PowerShell
// whole, with the session environment set, and its script is gone once it
// has run.
func TestPsmuxRunInPaneDeliversALongCommand(t *testing.T) {
	driver := requireTmux(t)
	id := psmuxSession(t, driver, "rerun", "Write-Output launched", "launched")
	marker := filepath.Join(t.TempDir(), "ran")
	payload := strings.Repeat("x", 1500)
	command := "[IO.File]::WriteAllText(" + ShellQuote(marker) + ", $env:AGENT_MANAGER_SESSION_ID + ' ' + " + ShellQuote(payload) + ")"
	if err := driver.RunInPane(id, map[string]string{"AGENT_MANAGER_SESSION_ID": "abc123"}, command); err != nil {
		t.Fatalf("RunInPane: %v", err)
	}
	want := "abc123 " + payload
	var got string
	for deadline := time.Now().Add(15 * time.Second); got != want && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		data, _ := os.ReadFile(marker)
		got = string(data)
	}
	if got != want {
		pane, _ := driver.CapturePane(id)
		t.Fatalf("the pane wrote %d bytes, want the session env and the %d-byte payload; pane:\n%s", len(got), len(payload), pane)
	}
	left, err := filepath.Glob(filepath.Join(os.TempDir(), "am-relaunch-"+id+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("relaunch scripts left behind: %v", left)
	}
}

// A prompt holding double quotes reaches the agent CLI as the one argument
// it was quoted as, not split around the quotes into flags of its own.
func TestPsmuxPromptWithQuotesStaysOneArgument(t *testing.T) {
	driver := requireTmux(t)
	if _, err := pwsh.Path(); err != nil {
		t.Skip(err)
	}
	file := filepath.Join(t.TempDir(), "args")
	prompt := `x " --dangerously-skip-permissions " y`
	command := "& " + ShellQuote(os.Args[0]) + " " + ShellQuote(prompt)
	id := prefixedID("quotes")
	if err := driver.Create(id, paneDir(), command, map[string]string{argsFileEnv: file}, 100, 30); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	var got []byte
	for deadline := time.Now().Add(15 * time.Second); got == nil && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		got, _ = os.ReadFile(file)
	}
	if string(got) != prompt {
		pane, _ := driver.CapturePane(id)
		t.Fatalf("the agent got arguments %q, want the one argument %q; pane:\n%s", strings.Split(string(got), "\n"), prompt, pane)
	}
}

func TestParsePsmuxVersion(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		wantErr string
	}{
		{"current release", "tmux 3.4.0\npsmux 3.4.0 (20ef00fb 2026-10-14)\n", ""},
		{"newer", "psmux 3.5.1", ""},
		{"too old", "psmux 3.3.8", "psmux 3.4.0"},
		{"no psmux line, the 3.3.x shape", "tmux 3.3.6", "unknown"},
		{"unparseable", "psmux banana", "banana"},
	}
	for _, tc := range cases {
		err := parsePsmuxVersion(tc.out)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: parsePsmuxVersion(%q) = %v, want nil", tc.name, tc.out, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: parsePsmuxVersion(%q) = nil, want an error", tc.name, tc.out)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantErr)
		}
	}
}
