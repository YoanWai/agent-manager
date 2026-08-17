package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestSetLabelNeutralizesFormatStrings(t *testing.T) {
	driver := requireTmux(t)
	id := "lbl" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
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
	driver := requireTmux(t)
	id := "send" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "cat", nil, 0, 0); err != nil {
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

func TestSendTextKeepsEnterOutsideBracketedPaste(t *testing.T) {
	driver := requireTmux(t)
	id := "bracket" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-bracket-" + id
	t.Cleanup(func() { os.Remove(marker) })

	text := "hello world"
	want := "\x1b[200~" + text + "\x1b[201~\r"
	command := "stty raw -echo; printf '\\033[?2004h'; dd bs=1 count=" +
		strconv.Itoa(len(want)) + " of=" + ShellQuote(marker) + " 2>/dev/null"
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	// Let tmux observe the application's bracketed-paste mode request before
	// delivering the message.
	time.Sleep(100 * time.Millisecond)
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
	driver := requireTmux(t)
	id := "late" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	reads := "/tmp/am-late-" + id
	t.Cleanup(func() { os.Remove(reads) })

	// Opens on a blank line, which still has to be waited for.
	text := "\nhello world"
	// Stalls before its first read, then logs each read between pipes and
	// echoes it back so the pane shows what it took.
	command := "stty raw -echo; printf '\\033[?2004h'; sleep 0.4; " +
		"while :; do dd bs=4096 count=1 2>/dev/null | tee -a " + ShellQuote(reads) +
		"; printf '|' >> " + ShellQuote(reads) + "; done"
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	time.Sleep(100 * time.Millisecond)
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
	if !strings.Contains(string(logged), "send-keys -t am_x1 Enter") {
		t.Fatalf("Enter never sent, calls:\n%s", logged)
	}
}

// A clipboard paste forwarded into a focused pane must arrive inside the
// pane's bracketed-paste markers with no Enter after it, so agent composers
// keep multi-line text instead of submitting on the first newline.
func TestPasteDeliversWithoutSubmitting(t *testing.T) {
	driver := requireTmux(t)
	id := "paste" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-paste-nosubmit-" + id
	t.Cleanup(func() { os.Remove(marker) })

	text := "first line\nsecond line\n"
	// paste-buffer converts newlines to carriage returns, the same bytes a
	// terminal emits when pasting; composers read them as line breaks.
	want := "\x1b[200~first line\rsecond line\r\x1b[201~"
	command := "stty raw -echo; printf '\\033[?2004h'; dd bs=1 count=" +
		strconv.Itoa(len(want)+1) + " of=" + ShellQuote(marker) + " 2>/dev/null"
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	time.Sleep(100 * time.Millisecond)
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

// Create used to type the launch line with send-keys, which silently
// truncates around 1024 bytes and left long first prompts as a broken
// shell command. paste-buffer must deliver the full line.
func TestCreateDeliversLongCommand(t *testing.T) {
	driver := requireTmux(t)
	id := "long" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := "/tmp/am-long-" + id
	t.Cleanup(func() { os.Remove(marker) })

	payload := strings.Repeat("x", 1500)
	command := "printf '%s' '" + payload + "' > " + marker
	if len(command) < 1024 {
		t.Fatalf("test command must exceed the old 1024-byte send-keys limit, got %d", len(command))
	}
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
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
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	t.Cleanup(func() {
		if err := driver.ClearRequest(); err != nil {
			t.Errorf("ClearRequest: %v", err)
		}
	})

	if err := driver.ClearRequest(); err != nil {
		t.Fatalf("ClearRequest: %v", err)
	}
	request, err := driver.PendingRequest()
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("no request expected on a clean marker, got %q", request)
	}

	for _, want := range []string{RequestReview, RequestEditor} {
		if _, err := tmuxCmd("set-option", "-g", requestOption, want).CombinedOutput(); err != nil {
			t.Fatalf("set marker: %v", err)
		}
		request, err = driver.PendingRequest()
		if err != nil {
			t.Fatalf("PendingRequest: %v", err)
		}
		if request != want {
			t.Fatalf("marker reads as %q, want %q", request, want)
		}

		if err := driver.ClearRequest(); err != nil {
			t.Fatalf("ClearRequest: %v", err)
		}
		request, err = driver.PendingRequest()
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
	if out, err := tmuxCmd("bind-key", "-n", "C-o", "display-message", "stale").CombinedOutput(); err != nil {
		t.Fatalf("seed the old binding: %v: %s", err, out)
	}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}

	bound, err := tmuxCmd("list-keys", "-T", "root").CombinedOutput()
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
	t.Cleanup(func() {
		if out, err := tmuxCmd("bind-key", "-T", "prefix", "d", "detach-client").CombinedOutput(); err != nil {
			t.Errorf("restore prefix d: %v: %s", err, out)
		}
	})
	if out, err := tmuxCmd("unbind-key", "-T", "prefix", "d").CombinedOutput(); err != nil {
		t.Fatalf("unbind prefix d: %v: %s", err, out)
	}

	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	bound, err := tmuxCmd("list-keys", "-T", "prefix").CombinedOutput()
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
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
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
	if strings.Contains(string(right), "None d") || !strings.Contains(string(right), "Ctrl+q = back") {
		t.Fatalf("footer should retain only the direct escape without prefixes, got %q", right)
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
// background with OSC 11. Nothing answers that on this server — the only
// client is in control mode and has no tty — unless the pane carries an
// explicit background of its own, which is what the pane theme sets.
func TestCreateAnswersBackgroundQuery(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "osc" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	reply := t.TempDir() + "/reply"
	// tmux delivers the answer on the pane's input, so the query and the
	// read both happen inside the pane. Raw mode keeps the line discipline
	// from holding a reply that ends in ST rather than a newline.
	command := "stty raw; printf '\\033]11;?\\033\\\\'; cat > " + reply
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	got := waitForFile(t, driver, id, reply)
	if want := "]11;rgb:1e1e/1e1e/2e2e"; !strings.Contains(got, want) {
		t.Fatalf("OSC 11 reply = %q, want one carrying %q", got, want)
	}
}

// COLORFGBG is the fallback for agents that read the environment instead of
// querying, and nothing in a pane's environment carries it otherwise.
func TestCreateExportsColorFgBg(t *testing.T) {
	driver := requireTmux(t)
	t.Cleanup(func() { clearPaneTheme(t) })
	driver.PublishPaneTheme(PaneTheme{Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "fgbg" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	marker := t.TempDir() + "/env"
	if err := driver.Create(id, "/tmp", "printenv COLORFGBG > "+marker, nil, 0, 0); err != nil {
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
	driver.PublishPaneTheme(PaneTheme{Background: "#1e1e2e", ColorFgBg: "15;0"})

	id := "pub" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if got, want := globalWindowStyle(t), "bg=#1e1e2e"; got != want {
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
	driver.PublishPaneTheme(PaneTheme{Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	backgrounds := []string{"#111111", "#222222", "#333333", "#444444", "#eff1f5"}
	var wg sync.WaitGroup
	for _, bg := range backgrounds {
		driver.PublishPaneTheme(PaneTheme{Background: bg, ColorFgBg: "15;0"})
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
	if got, want := globalWindowStyle(t), "bg="+last; got != want {
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
	driver.PublishPaneTheme(PaneTheme{Background: "#101010", ColorFgBg: "15;0"})
	if err := driver.Create(hold, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create hold session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(hold) })

	const newer = "#eff1f5"
	var pushed sync.WaitGroup
	afterCreateThemeLoad = func() {
		driver.PublishPaneTheme(PaneTheme{Background: newer, ColorFgBg: "0;15"})
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
	if err := driver.Create(raced, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create raced session: %v", err)
	}
	t.Cleanup(func() { driver.Kill(raced) })
	pushed.Wait()

	if got, want := globalWindowStyle(t), "bg="+newer; got != want {
		t.Fatalf("window-style = %q, want the newer pushed theme %q", got, want)
	}
}

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
	if panes[id] <= 0 {
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
