package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// skipPOSIXShell skips a test whose fixture only a POSIX shell runs.
func skipPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX shell script")
	}
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
	if err := driver.Create(id, paneDir(), "", nil, 100, 30); err != nil {
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
	skipPOSIXShell(t)
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

// resolvedOption's global fallback can fail on its own (no server, a
// stale socket); that error must reach the caller, not just the
// session-scoped read's.
func TestResolvedOptionPropagatesTheGlobalFallbackError(t *testing.T) {
	skipPOSIXShell(t)
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *'-g -v prefix'*) echo 'no server running' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if _, err := driver.resolvedOption("x1", "prefix"); err == nil {
		t.Fatal("resolvedOption should propagate the global fallback's error")
	}
}

// With no server up, list-keys still answers from a server that exits
// straight after, and only the command list finds none. That is not an
// error: Create installs the bindings once a session starts the server.
func TestEnsureBindingsIgnoresAMissingServer(t *testing.T) {
	skipPOSIXShell(t)
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *list-keys*) exit 0;; esac\necho 'no server running on /tmp/agentmgr' >&2; exit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings with no server: %v", err)
	}
}

// Create runs on the UI's update path, and a new session cannot carry a pin,
// so with tmux_prefix off only a refresh looks for one.
func TestOnlyARefreshLooksForAPinnedPrefix(t *testing.T) {
	skipPOSIXShell(t)
	dir := t.TempDir()
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$*\" in *" + pinnedPrefixOption + "*) echo 'pin read' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if err := driver.installSessionUX("am_new"); err != nil {
		t.Fatalf("installing a session with tmux_prefix off should not read the pin: %v", err)
	}
	if err := driver.RefreshChrome("live"); err == nil || !strings.Contains(err.Error(), "pin read") {
		t.Fatalf("a refresh should look for a pin to take off, err = %v", err)
	}
}

func TestSetLabelNeutralizesFormatStrings(t *testing.T) {
	driver := requireTmux(t)
	id := "lbl" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	marker := "/tmp/am-injection-" + id
	if err := driver.SetLabel(id, "evil #(touch "+marker+") name"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	rendered, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-left}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	if !strings.Contains(string(rendered), "#(touch") {
		t.Fatalf("format string should render literally, got %q", rendered)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		os.Remove(marker)
		t.Fatal("injection executed: marker file was created")
	}
}

func TestSendText(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "send" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "cat", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if err := driver.SendText(id, "hello world"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		var err error
		pane, err = driver.CapturePane(id)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		if strings.Contains(pane, "hello world") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(pane, "hello world") {
		t.Fatalf("cat should echo the sent line, pane: %q", pane)
	}
}

// pasteReady follows the paste-mode request, so a pane showing it has bracketed paste on.
const pasteReady = "paste-ready"

func TestSendTextKeepsEnterOutsideBracketedPaste(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "bracket" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-bracket-" + id
	t.Cleanup(func() { os.Remove(marker) })

	text := "hello world"
	want := "\x1b[200~" + text + "\x1b[201~\r"
	command := "stty raw -echo; printf '\\033[?2004h" + pasteReady + "'; dd bs=1 count=" +
		strconv.Itoa(len(want)) + " of=" + ShellQuote(marker) + " 2>/dev/null"
	if err := driver.Create(id, paneDir(), command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	waitForPane(t, driver, id, pasteReady)
	if err := driver.SendText(id, text); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := os.ReadFile(marker)
		if err == nil && string(got) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	got, _ := os.ReadFile(marker)
	t.Fatalf("pane input = %q, want bracketed paste followed by Enter %q", got, want)
}

// A pane too busy to read between the two writes takes the carriage return
// as part of the bracketed paste, so the Enter has to reach it as a read of
// its own, however late the pane gets around to reading.
func TestSendTextSubmitsIntoAPaneThatReadsLate(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "late" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	reads := "/tmp/am-late-" + id
	t.Cleanup(func() { os.Remove(reads) })

	// Opens on a blank line, which still has to be waited for.
	text := "\nhello world"
	// Stalls before its first read, then logs each read between pipes and
	// echoes it back so the pane shows what it took.
	command := "stty raw -echo; printf '\\033[?2004h" + pasteReady + "'; sleep 0.4; " +
		"while :; do dd bs=4096 count=1 2>/dev/null | tee -a " + ShellQuote(reads) +
		"; printf '|' >> " + ShellQuote(reads) + "; done"
	if err := driver.Create(id, paneDir(), command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	waitForPane(t, driver, id, pasteReady)
	if err := driver.SendText(id, text); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	want := "\x1b[201~|\r"
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got, err := os.ReadFile(reads); err == nil && strings.Contains(string(got), want) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	got, _ := os.ReadFile(reads)
	t.Fatalf("pane reads = %q, want the paste to end a read (%q) before the Enter", got, want)
}

// A capture that fails leaves no baseline to measure the paste against, and
// text already on screen would pass for it, so the Enter waits the window out
// rather than matching against nothing.
func TestSendTextWaitsOutTheWindowWhenTheBaselineCaptureFails(t *testing.T) {
	skipPOSIXShell(t)
	dir := t.TempDir()
	callLog := dir + "/calls"
	stub := dir + "/tmux"
	// Fails the first capture, then answers every later one with a pane that
	// already holds the message.
	script := "#!/bin/sh\necho \"$@\" >> " + callLog + "\n" +
		"case \"$*\" in *capture-pane*)\n" +
		"  [ \"$(grep -c capture-pane " + callLog + ")\" = 1 ] && exit 1\n" +
		"  echo 'hello world';;\nesac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	start := time.Now()
	if err := driver.SendText("x1", "hello world"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if elapsed := time.Since(start); elapsed < echoWait {
		t.Fatalf("Enter went out after %v, want the pane to get its full %v", elapsed, echoWait)
	}
	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	if !strings.Contains(string(logged), "send-keys -t "+PaneTarget("x1")+" Enter") {
		t.Fatalf("Enter never sent, calls:\n%s", logged)
	}
}

// A clipboard paste forwarded into a focused pane must arrive inside the
// pane's bracketed-paste markers with no Enter after it, so agent composers
// keep multi-line text instead of submitting on the first newline.
func TestPasteDeliversWithoutSubmitting(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "paste" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-paste-nosubmit-" + id
	t.Cleanup(func() { os.Remove(marker) })

	text := "first line\nsecond line\n"
	// paste-buffer converts newlines to carriage returns, the same bytes a
	// terminal emits when pasting; composers read them as line breaks.
	want := "\x1b[200~first line\rsecond line\r\x1b[201~"
	command := "stty raw -echo; printf '\\033[?2004h" + pasteReady + "'; dd bs=1 count=" +
		strconv.Itoa(len(want)+1) + " of=" + ShellQuote(marker) + " 2>/dev/null"
	if err := driver.Create(id, paneDir(), command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	waitForPane(t, driver, id, pasteReady)
	if err := driver.Paste(id, text); err != nil {
		t.Fatalf("Paste: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := os.ReadFile(marker)
		if err == nil && string(got) == want {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got, _ := os.ReadFile(marker); string(got) != want {
		t.Fatalf("pane input = %q, want bracketed paste %q", got, want)
	}
	// dd is still waiting for one more byte; a stray Enter would land now.
	time.Sleep(300 * time.Millisecond)
	if got, _ := os.ReadFile(marker); string(got) != want {
		t.Fatalf("pane received extra input after the paste: %q", got)
	}
}

// tmux keeps paste buffers per server, where the manager and every agent's MCP
// process paste side by side.
func TestPasteBufferNamesDifferAcrossProcesses(t *testing.T) {
	skipPOSIXShell(t)
	dir := t.TempDir()
	callLog := dir + "/calls"
	stub := dir + "/tmux"
	script := "#!/bin/sh\ncase \"$3\" in load-buffer) echo \"$5\" >> " + callLog + ";; esac\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}
	for range 2 {
		if err := <-startOtherProcess(t, driver, "paste", "x1"); err != nil {
			t.Fatalf("other process: %v", err)
		}
	}
	loaded := map[string]bool{}
	for _, name := range readCalls(t, callLog) {
		if loaded[name] {
			t.Fatalf("two pastes loaded buffer %s", name)
		}
		loaded[name] = true
	}
}

// Create used to type the launch line with send-keys, which silently
// truncates around 1024 bytes and left long first prompts as a broken
// shell command. paste-buffer must deliver the full line.
func TestCreateDeliversLongCommand(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "long" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-long-" + id
	t.Cleanup(func() { os.Remove(marker) })

	payload := strings.Repeat("x", 1500)
	command := "printf '%s' '" + payload + "' > " + marker
	if len(command) < 1024 {
		t.Fatalf("test command must exceed the old 1024-byte send-keys limit, got %d", len(command))
	}
	if err := driver.Create(id, paneDir(), command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil && string(data) == payload {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane, _ := driver.CapturePane(id)
	got, _ := os.ReadFile(marker)
	t.Fatalf("long launch command truncated or failed; wrote %d bytes, want %d; pane:\n%s", len(got), len(payload), pane)
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

// The editor key moved off C-o, which the agents running inside a session
// bind themselves. A server that predates the move still carries the old
// binding, so EnsureBindings has to drop it as well as install F3.
func TestEnsureBindingsMovesTheEditorKeyToF3(t *testing.T) {
	driver := requireTmux(t)
	server := bindingServer(t, driver)
	oldKey := bindingOf(t, "ctrl+o").Keys()[0]
	stale := append([][]string{rootBinding(oldKey, "set-option -g "+requestOption+" "+RequestEditor+" ; detach-client")},
		recordRootBindings([]string{oldKey.Tmux()})...)
	for _, command := range stale {
		if out, err := server(command...).CombinedOutput(); err != nil {
			t.Fatalf("seed the old binding: %v: %s", err, out)
		}
	}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}

	bound, err := server("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	editor := false
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if fields[3] == "C-o" {
			t.Fatalf("C-o should be unbound, got %q", line)
		}
		if fields[3] == "F3" && strings.Contains(line, RequestEditor) {
			editor = true
		}
	}
	if !editor {
		t.Fatalf("F3 should request the editor, got %q", bound)
	}
}

func TestEnsureBindingsRestoresPrefixDetach(t *testing.T) {
	driver := requireTmux(t)
	server := bindingServer(t, driver)
	t.Cleanup(func() {
		if out, err := server("bind-key", "-T", "prefix", "d", "detach-client").CombinedOutput(); err != nil {
			t.Errorf("restore prefix d: %v: %s", err, out)
		}
	})
	if out, err := server("unbind-key", "-T", "prefix", "d").CombinedOutput(); err != nil {
		t.Fatalf("unbind prefix d: %v: %s", err, out)
	}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	bound, err := server("list-keys", "-T", "prefix").CombinedOutput()
	if err != nil {
		t.Fatalf("list prefix d: %v: %s", err, bound)
	}
	found := false
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "bind-key" && fields[1] == "-T" && fields[2] == "prefix" && fields[3] == "d" && fields[4] == "detach-client" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("prefix d should detach, got %q", bound)
	}
}

func TestRefreshChromeKeepsLabelAndAddsSessionHints(t *testing.T) {
	driver := requireTmux(t)
	id := "chr" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if err := driver.SetLabel(id, "my-session"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", `C-\`).CombinedOutput(); err != nil {
		t.Fatalf("set prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome: %v", err)
	}

	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if !strings.Contains(string(right), "Ctrl+r = review") {
		t.Fatalf("footer should advertise review, got %q", right)
	}
	if !strings.Contains(string(right), `C-\ d = back`) {
		t.Fatalf("footer should advertise the configured-prefix escape, got %q", right)
	}
	if !strings.Contains(string(right), `Ctrl+q / C-\ d = back`) {
		t.Fatalf("footer should advertise the available direct escape, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "C-q").CombinedOutput(); err != nil {
		t.Fatalf("set conflicting prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome with conflicting prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("conflicting status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q /") {
		t.Fatalf("footer should hide a direct shortcut claimed by the prefix, got %q", right)
	}
	if !strings.Contains(string(right), "C-q d = back") {
		t.Fatalf("footer should retain the prefix escape, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", `C-\`).CombinedOutput(); err != nil {
		t.Fatalf("restore primary prefix: %v: %s", err, out)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix2", "C-q").CombinedOutput(); err != nil {
		t.Fatalf("set conflicting secondary prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome with conflicting secondary prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("secondary-prefix status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q /") {
		t.Fatalf("footer should hide a direct shortcut claimed by prefix2, got %q", right)
	}
	if !strings.Contains(string(right), `C-\ d = back`) {
		t.Fatalf("footer should retain the primary-prefix escape, got %q", right)
	}
	// psmux has no None prefix: it ignores the value and keeps the old key.
	if runtime.GOOS == "windows" {
		return
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "None").CombinedOutput(); err != nil {
		t.Fatalf("disable prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome without prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("prefix-free status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q") {
		t.Fatalf("footer should hide a direct shortcut claimed by prefix2, got %q", right)
	}
	if !strings.Contains(string(right), "C-q d = back") {
		t.Fatalf("footer should show prefix2 when the primary prefix is disabled, got %q", right)
	}
	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix2", "None").CombinedOutput(); err != nil {
		t.Fatalf("disable secondary prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome without either prefix: %v", err)
	}
	right, err = tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("prefix-free status-right: %v", err)
	}
	if strings.Contains(string(right), "None d") || !strings.Contains(string(right), `Ctrl+q / Ctrl+\ = back`) {
		t.Fatalf("footer should retain only the direct escapes without prefixes, got %q", right)
	}
	length, err := tmuxCmd("show-option", "-t", "am_"+id, "-v", "status-right-length").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right-length: %v", err)
	}
	if strings.TrimSpace(string(length)) != "100" {
		t.Fatalf("status-right-length should fit the footer, got %q", length)
	}
	left, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-left}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-left: %v", err)
	}
	if !strings.Contains(string(left), "my-session") {
		t.Fatalf("re-styling should keep the name label, got %q", left)
	}
}

// A tmux.conf almost always sets the prefix with "set -g", which
// TestRefreshChromeKeepsLabelAndAddsSessionHints never exercises (it
// always overrides "-t <session>" directly).
func TestRefreshChromeResolvesAGloballySetPrefix(t *testing.T) {
	driver := requireTmux(t)
	original, err := tmuxCmd("show-options", "-g", "-v", "prefix").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options prefix: %v: %s", err, original)
	}
	snapshot := strings.TrimSpace(string(original))
	t.Cleanup(func() {
		if out, err := tmuxCmd("set-option", "-g", "prefix", snapshot).CombinedOutput(); err != nil {
			t.Errorf("restore prefix: %v: %s", err, out)
		}
	})

	id := "globalprefix" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if out, err := tmuxCmd("set-option", "-g", "prefix", "C-q").CombinedOutput(); err != nil {
		t.Fatalf("set global prefix: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome: %v", err)
	}

	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if strings.Contains(string(right), "Ctrl+q /") {
		t.Fatalf("footer should hide the default detach key claimed by a globally-set prefix, got %q", right)
	}
	if !strings.Contains(string(right), "C-q d = back") {
		t.Fatalf("footer should advertise the prefix escape for a globally-set prefix, got %q", right)
	}
}

// tmux.conf's prefix beats every binding, so tmux_prefix pins another one to free that key.
func TestTmuxPrefixFreesTheServersPrefixForASessionKey(t *testing.T) {
	driver := requireTmux(t)
	restoreDefaultKeys(t, driver)
	original, err := tmuxCmd("show-options", "-g", "-v", "prefix").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options prefix: %v: %s", err, original)
	}
	t.Cleanup(func() {
		if out, err := tmuxCmd("set-option", "-g", "prefix", strings.TrimSpace(string(original))).CombinedOutput(); err != nil {
			t.Errorf("restore prefix: %v: %s", err, out)
		}
	})
	detachOnCtrlS := keybind.DefaultSession().With(keybind.Detach, bindingOf(t, "ctrl+s"))
	driver.SetSessionKeys(detachOnCtrlS.With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b")))
	id := "tmuxprefix" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	sessionOption := func(option string) string {
		t.Helper()
		out, err := tmuxCmd("show-options", "-q", "-v", "-t", "am_"+id, option).CombinedOutput()
		if err != nil {
			t.Fatalf("show-options %s: %v: %s", option, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	footer := func() string {
		t.Helper()
		right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
		if err != nil {
			t.Fatalf("status-right: %v: %s", err, right)
		}
		return string(right)
	}
	// psmux keeps global and session options in one store per session
	// server, so a server-global prefix cannot sit under a session pin the
	// way tmux.conf's does on tmux; the pin is the only prefix there.
	// psmux also has no None prefix: it normalizes the marker to none, and
	// an unset session prefix answers the server's own default, not empty.
	if runtime.GOOS == "windows" {
		if got := sessionOption("prefix") + " " + sessionOption("prefix2"); got != "C-b none" {
			t.Fatalf("the session should answer to the pinned prefix alone, got %q", got)
		}
		driver.SetSessionKeys(detachOnCtrlS)
		if err := driver.RefreshChrome(id); err != nil {
			t.Fatalf("RefreshChrome without tmux_prefix: %v", err)
		}
		if got := sessionOption("prefix"); got != "C-b" {
			t.Fatalf("clearing tmux_prefix should hand back the server's default prefix, the session still sets %q", got)
		}
		if right := footer(); !strings.Contains(right, "Ctrl+s / C-b d = back") {
			t.Fatalf("after clearing tmux_prefix the footer should advertise the free key and the default prefix, got %q", right)
		}
		if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "C-a").CombinedOutput(); err != nil {
			t.Fatalf("set a session prefix by hand: %v: %s", err, out)
		}
		if err := driver.RefreshChrome(id); err != nil {
			t.Fatalf("RefreshChrome over a hand-set prefix: %v", err)
		}
		if got := sessionOption("prefix"); got != "C-a" {
			t.Fatalf("a prefix the manager never set should stay, got %q", got)
		}
		return
	}
	if out, err := tmuxCmd("set-option", "-g", "prefix", "C-s").CombinedOutput(); err != nil {
		t.Fatalf("set the tmux.conf prefix: %v: %s", err, out)
	}
	if got := sessionOption("prefix") + " " + sessionOption("prefix2"); got != "C-b None" {
		t.Fatalf("the session should answer to the pinned prefix alone, got %q", got)
	}
	if right := footer(); !strings.Contains(right, "Ctrl+s / C-b d = back") {
		t.Fatalf("ctrl+s should be free to detach, got %q", right)
	}

	driver.SetSessionKeys(detachOnCtrlS)
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome without tmux_prefix: %v", err)
	}
	if got := sessionOption("prefix"); got != "" {
		t.Fatalf("clearing tmux_prefix should hand back the server's prefix, the session still sets %q", got)
	}
	if right := footer(); strings.Contains(right, "Ctrl+s /") || !strings.Contains(right, "C-s d = back") {
		t.Fatalf("the server's prefix should shadow ctrl+s again, got %q", right)
	}

	if out, err := tmuxCmd("set-option", "-t", "am_"+id, "prefix", "C-a").CombinedOutput(); err != nil {
		t.Fatalf("set a session prefix by hand: %v: %s", err, out)
	}
	if err := driver.RefreshChrome(id); err != nil {
		t.Fatalf("RefreshChrome over a hand-set prefix: %v", err)
	}
	if got := sessionOption("prefix"); got != "C-a" {
		t.Fatalf("a prefix the manager never set should stay, got %q", got)
	}
}

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

// An agent that auto-detects its palette asks the terminal for its
// foreground and background with OSC 10 and 11. Nothing answers that on this
// server — the only client is in control mode and has no tty — unless the
// pane carries explicit colors of its own, which is what the pane theme sets.
func TestCreateAnswersColorQueries(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "osc" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	reply := t.TempDir() + "/reply"
	// tmux delivers the answer on the pane's input, so the query and the
	// read both happen inside the pane. Raw mode keeps the line discipline
	// from holding a reply that ends in ST rather than a newline.
	command := "stty raw; printf '\\033]10;?\\033\\\\\\033]11;?\\033\\\\'; cat > " + reply
	if err := driver.Create(id, paneDir(), command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	got := waitForFile(t, driver, id, reply)
	for _, want := range []string{"]10;rgb:cdcd/d6d6/f4f4", "]11;rgb:1e1e/1e1e/2e2e"} {
		if !strings.Contains(got, want) {
			t.Fatalf("OSC 10/11 replies = %q, want one carrying %q", got, want)
		}
	}
}

// COLORFGBG is the fallback for agents that read the environment instead of
// querying, and nothing in a pane's environment carries it otherwise.
func TestCreateExportsColorFgBg(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "fgbg" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := t.TempDir() + "/env"
	if err := driver.Create(id, paneDir(), "printenv COLORFGBG > "+marker, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got := strings.TrimSpace(waitForFile(t, driver, id, marker)); got != "15;0" {
		t.Fatalf("COLORFGBG in pane = %q, want %q", got, "15;0")
	}
}

func globalWindowStyle(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("show-options", "-gv", "window-style").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options window-style: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// A session created after a theme is published, but before any push has run,
// still opens on that theme: Create applies the recorded value in its own
// command list rather than relying on a separate push having landed.
func TestCreateAppliesPublishedThemeWithoutAPush(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "pub" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg=#1e1e2e"; got != want {
		t.Fatalf("window-style = %q, want %q", got, want)
	}
}

// Concurrent pushes are latest-wins: whichever runs last writes the theme
// published last, not an older one it was spawned for. The lock serializes
// the writes and each push sends the current published value, so the server
// settles on the final publish however the goroutines interleave.
func TestPushPaneThemeIsLatestWins(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })

	// A session with no windows exits at once, so one holds the server up
	// for the global option to stick to.
	id := "race" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	backgrounds := []string{"#111111", "#222222", "#333333", "#444444", "#eff1f5"}
	var wg sync.WaitGroup
	for _, bg := range backgrounds {
		driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: bg, ColorFgBg: "15;0"})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := driver.PushPaneTheme(); err != nil {
				t.Errorf("PushPaneTheme: %v", err)
			}
		}()
	}
	wg.Wait()

	last := backgrounds[len(backgrounds)-1]
	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg="+last; got != want {
		t.Fatalf("window-style = %q, want the last published theme %q", got, want)
	}
}

// Create shares the push lock with PushPaneTheme, so a session opened while a
// newer theme is being pushed cannot reset the server to the theme Create
// loaded. The seam runs while Create holds the lock: it publishes a newer
// theme and starts a push, which blocks on the lock until Create's write
// lands, then writes the newer theme last. Without the shared lock the push
// runs during the seam and Create's stale write clobbers it.
func TestCreateSerializesWithPushPaneTheme(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })

	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	hold := "hold" + stamp
	driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(hold, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create hold session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(hold) })

	const newer = "#eff1f5"
	var pushed sync.WaitGroup
	afterCreateThemeLoad = func() {
		driver.PublishPaneTheme(PaneTheme{Foreground: "#cdd6f4", Background: newer, ColorFgBg: "0;15"})
		pushed.Add(1)
		go func() {
			defer pushed.Done()
			if err := driver.PushPaneTheme(); err != nil {
				t.Errorf("PushPaneTheme: %v", err)
			}
		}()
		// Give the push goroutine time to reach the lock, so the serialization
		// under test is what orders the two writes rather than this timing.
		time.Sleep(100 * time.Millisecond)
	}
	t.Cleanup(func() { afterCreateThemeLoad = nil })

	raced := "raced" + stamp
	if err := driver.Create(raced, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create raced session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(raced) })
	pushed.Wait()

	if got, want := globalWindowStyle(t), "fg=#cdd6f4,bg="+newer; got != want {
		t.Fatalf("window-style = %q, want the newer pushed theme %q", got, want)
	}
}

func TestLifecycle(t *testing.T) {
	skipPOSIXShell(t)
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

// The session environment has to outlive the launch command. Quitting the
// agent drops the pane onto the shell the script execs, and an agent
// started again from that shell belongs to this managed session only if it
// inherits these values: the rename subcommand and the MCP server both
// identify the session by AGENT_MANAGER_SESSION_ID alone.
func TestCreateExportsSessionEnvIntoTheShell(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "senv" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := t.TempDir() + "/env"
	env := map[string]string{
		"AGENT_MANAGER_SESSION_ID":  "abc123",
		"AGENT_MANAGER_STATUS_FILE": "/tmp/status-abc123",
	}
	// A launch command that returns at once stands in for the user quitting
	// the agent back to the pane's shell.
	if err := driver.Create(id, paneDir(), "printf 'agent ran\\n'", env, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	waitForPane(t, driver, id, relaunchHint)

	// Written whole and moved into place, so the read cannot land between
	// the two values.
	report := `printf '%s %s\n' "$AGENT_MANAGER_SESSION_ID" "$AGENT_MANAGER_STATUS_FILE" > ` +
		marker + `.part && mv ` + marker + `.part ` + marker
	if err := driver.SendKeys(id, report, "Enter"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	got := strings.Fields(waitForFile(t, driver, id, marker))
	want := []string{"abc123", "/tmp/status-abc123"}
	if !slices.Equal(got, want) {
		t.Fatalf("environment left to the shell = %v, want %v", got, want)
	}
}

// The pane is where the user is looking when the agent exits, so the way
// back to a wired agent is named there.
func TestCreateNamesTheWayBackWhenTheAgentExits(t *testing.T) {
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "hint" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "printf 'agent ran\\n'", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	waitForPane(t, driver, id, relaunchHint)
}

// An agent that changes directory mid-session, as Claude Code's /cd and
// EnterWorktree do, is a child of the launch script, and tmux reports the
// directory of the pane's foreground process group.
func TestPanesFollowsTheAgentIntoANewDirectory(t *testing.T) {
	skipPOSIXShell(t)
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

func TestExportEnvPrefixesTheCommand(t *testing.T) {
	skipPOSIXShell(t)
	env := map[string]string{"B": "second", "A": "fir st"}
	want := `export A='fir st'; export B='second'; claude --resume 7`
	if got := ExportEnv(env, "claude --resume 7"); got != want {
		t.Fatalf("ExportEnv = %q, want %q", got, want)
	}
}

// An agent is free to split its own window and leave the new pane focused,
// which Claude Code's agent teams do when they run a teammate beside their
// leader. Everything the manager reads and types has to stay on the agent's
// own pane through that.
func TestManagerStaysOnTheAgentPaneAfterASplit(t *testing.T) {
	skipPOSIXShell(t)
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

// tmux answers a pane target whose index does not exist with the active
// pane rather than an error, so a user config that numbers panes from 1
// would point every capture and keystroke at whatever pane has focus.
func TestEnsureBindingsPinsPaneNumbering(t *testing.T) {
	driver := requireTmux(t)
	server := bindingServer(t, driver)
	if out, err := server("set-window-option", "-g", "pane-base-index", "1").CombinedOutput(); err != nil {
		t.Fatalf("set pane-base-index: %v: %s", err, out)
	}
	t.Cleanup(func() { server("set-window-option", "-g", "pane-base-index", "0").Run() })

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}

	out, err := server("show-window-options", "-gv", "pane-base-index").CombinedOutput()
	if err != nil {
		t.Fatalf("show-window-options: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "0" {
		t.Fatalf("pane-base-index = %q, want 0", got)
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
	skipPOSIXShell(t)
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
				if err := driver.Create(id, paneDir(), "cat", nil, 80, 24); err != nil {
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
	skipPOSIXShell(t)
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
	skipPOSIXShell(t)
	driver := requireTmux(t)
	id := "window" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "cat", nil, 80, 24); err != nil {
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
	skipPOSIXShell(t)
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
func ownedRootLines(t *testing.T, server func(args ...string) *exec.Cmd) map[string]string {
	t.Helper()
	bound, err := server("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	// tmux bindings carry the session test; psmux's are recorded by key.
	recorded := map[string]bool{}
	if runtime.GOOS == "windows" {
		keys, err := server("show-option", "-gqv", rootKeysOption).CombinedOutput()
		if err != nil {
			t.Fatalf("recorded root keys: %v: %s", err, keys)
		}
		for _, key := range strings.Fields(string(keys)) {
			recorded[key] = true
		}
	}
	owned := map[string]string{}
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		key := strings.ReplaceAll(fields[3], `\\`, `\`)
		if runtime.GOOS == "windows" && recorded[key] || strings.Contains(line, ownedBindingTest) {
			owned[key] = line
		}
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

// The server outlives a config change, so the bindings the manager installs
// are the table's and nothing older: a key moved or turned off comes off
// the server, and moving back drops the keys it had moved to.
func TestEnsureBindingsFollowsTheKeyTable(t *testing.T) {
	driver := requireTmux(t)
	server := bindingServer(t, driver)
	restoreDefaultKeys(t, driver)
	custom := sessionOf(t, []string{"f9", "alt+q"}, []string{"ctrl+g"}, nil)
	driver.SetSessionKeys(custom)
	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	owned := ownedRootLines(t, server)
	for _, key := range []string{"F9", "M-q"} {
		// psmux bindings have no other session to pass the key through to.
		passes := runtime.GOOS == "windows" || strings.Contains(owned[key], "send-keys "+key)
		if line := owned[key]; !strings.Contains(line, "detach-client") || !passes {
			t.Errorf("%s should detach inside a session and pass through elsewhere, got %q", key, line)
		}
	}
	if line := owned["C-g"]; !strings.Contains(line, RequestReview) {
		t.Errorf("C-g should request the review, got %q", line)
	}
	for _, key := range []string{"C-q", `C-\`, "C-r", "F3"} {
		if line, bound := owned[key]; bound {
			t.Errorf("%s is off the table and should be unbound, got %q", key, line)
		}
	}

	driver.SetSessionKeys(keybind.DefaultSession())
	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings with defaults: %v", err)
	}
	owned = ownedRootLines(t, server)
	for _, key := range []string{"F9", "M-q", "C-g"} {
		if line, bound := owned[key]; bound {
			t.Errorf("%s should come off with the table that bound it, got %q", key, line)
		}
	}
	// tmux lists the stored branch with its backslashes doubled; psmux
	// binds the key straight to detach-client.
	passThrough := `send-keys C-\\\\`
	if runtime.GOOS == "windows" {
		passThrough = "detach-client"
	}
	if line := owned[`C-\`]; !strings.Contains(line, "detach-client") || !strings.Contains(line, passThrough) {
		t.Errorf(`C-\ should detach and pass itself through, got %q`, line)
	}
	if line := owned["C-q"]; !strings.Contains(line, "detach-client") {
		t.Errorf("C-q should detach, got %q", line)
	}
	if line := owned["C-r"]; !strings.Contains(line, RequestReview) {
		t.Errorf("C-r should request the review, got %q", line)
	}
	if line := owned["F3"]; !strings.Contains(line, RequestEditor) {
		t.Errorf("F3 should request the editor, got %q", line)
	}
}

// Rebinding drops only what the manager put there: the user's own
// tmux.conf loads on this server too, and its bindings are not ours to
// remove.
func TestEnsureBindingsLeavesTheUsersOwnBindingsAlone(t *testing.T) {
	driver := requireTmux(t)
	server := bindingServer(t, driver)
	if out, err := server("bind-key", "-n", "F9", "display-message", "mine").CombinedOutput(); err != nil {
		t.Fatalf("seed the user binding: %v: %s", err, out)
	}
	t.Cleanup(func() { server("unbind-key", "-n", "F9").Run() })

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	bound, err := server("list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	if !strings.Contains(string(bound), "display-message mine") {
		t.Fatalf("the user's F9 binding should survive, got:\n%s", bound)
	}
}

func TestAttachStatusRightNamesTheKeyTable(t *testing.T) {
	custom := sessionOf(t, []string{"f9", "alt+q"}, []string{"ctrl+g"}, nil)
	for _, tc := range []struct {
		name, primary, secondary string
		keys                     keybind.Table
		want                     string
	}{
		{"defaults", "C-b", "None", keybind.DefaultSession(), ` agent-manager · Ctrl+r = review · F3 = editor · Ctrl+q / Ctrl+\ / C-b d = back `},
		{"no prefix", "None", "None", keybind.DefaultSession(), ` agent-manager · Ctrl+r = review · F3 = editor · Ctrl+q / Ctrl+\ = back `},
		{"custom", "C-b", "None", custom, " agent-manager · Ctrl+g = review · F9 / Alt+q / C-b d = back "},
		{"prefix shadows a custom key", "M-q", "None", custom, " agent-manager · Ctrl+g = review · F9 / M-q d = back "},
	} {
		if got := attachStatusRight(tc.primary, tc.secondary, tc.keys); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// A session created under a custom table carries that table in its footer.
func TestSessionFooterNamesTheConfiguredKeys(t *testing.T) {
	driver := requireTmux(t)
	restoreDefaultKeys(t, driver)
	driver.SetSessionKeys(sessionOf(t, []string{"f9"}, []string{"ctrl+g"}, nil))
	id := "footerkeys"
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	footer := string(right)
	if !strings.Contains(footer, "Ctrl+g = review") || !strings.Contains(footer, "F9") || strings.Contains(footer, "editor") || strings.Contains(footer, "Ctrl+q") {
		t.Fatalf("footer should name the configured keys only, got %q", footer)
	}
}

func serverPid(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "#{pid}").CombinedOutput()
	if err != nil {
		t.Fatalf("server pid: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
