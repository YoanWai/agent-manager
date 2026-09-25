package tmux

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
	command := "stty raw -echo; printf '\\033[?2004h" + pasteReady + "'; dd bs=1 count=" +
		strconv.Itoa(len(want)) + " of=" + ShellQuote(marker) + " 2>/dev/null"
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
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
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
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
	if err := driver.Create(id, "/tmp", command, nil, 0, 0); err != nil {
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
