package notify

import (
	"errors"
	"strings"
	"testing"
)

func restore() func() {
	origGOOS, origEnv, origLook, origRun, origEmit := goos, getenv, lookPath, runCmd, emitSeq
	return func() {
		goos, getenv, lookPath, runCmd, emitSeq = origGOOS, origEnv, origLook, origRun, origEmit
	}
}

// cmdRecorder answers lookPath and runCmd while logging every command.
type cmdRecorder struct {
	known  map[string]bool
	called [][]string
}

func (r *cmdRecorder) install() {
	lookPath = func(name string) (string, error) {
		if r.known[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	runCmd = func(name string, args ...string) error {
		r.called = append(r.called, append([]string{name}, args...))
		return nil
	}
}

func TestNotifyDarwinGhosttyUsesOSC777(t *testing.T) {
	defer restore()()
	goos = "darwin"
	getenv = func(key string) string {
		if key == "TERM_PROGRAM" {
			return "ghostty"
		}
		return ""
	}
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	var emitted []string
	emitSeq = func(seq string) error {
		emitted = append(emitted, seq)
		return nil
	}
	Notify("deploy", "Waiting for your input")
	if len(emitted) != 1 || emitted[0] != "\x1b]777;notify;deploy;Waiting for your input\a" {
		t.Fatalf("want one OSC 777 sequence, got %q", emitted)
	}
	if len(rec.called) != 0 {
		t.Fatalf("osascript should not run inside Ghostty, got %v", rec.called)
	}
}

func TestNotifyDarwinPlainTerminalUsesAppleScript(t *testing.T) {
	defer restore()()
	goos = "darwin"
	getenv = func(string) string { return "" }
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	emitSeq = func(string) error { return nil }
	Notify("deploy", "Finished")
	if len(rec.called) != 1 || rec.called[0][0] != "osascript" {
		t.Fatalf("want one osascript call, got %v", rec.called)
	}
	call := rec.called[0]
	if call[len(call)-2] != "deploy" || call[len(call)-1] != "Finished" {
		t.Fatalf("title and body should ride argv unquoted, got %v", call)
	}
}

func TestNotifyLinuxUsesNotifySend(t *testing.T) {
	defer restore()()
	goos = "linux"
	getenv = func(string) string { return "" }
	rec := &cmdRecorder{known: map[string]bool{"notify-send": true}}
	rec.install()
	emitSeq = func(string) error { return nil }
	Notify("deploy", "Errored")
	if len(rec.called) != 1 {
		t.Fatalf("want one notify-send call, got %v", rec.called)
	}
	call := rec.called[0]
	if call[0] != "notify-send" || call[1] != "deploy" || call[2] != "Errored" {
		t.Fatalf("unexpected notify-send args %v", call)
	}
}

// Over plain SSH only TERM crosses to the remote host, so a Ghostty user
// working on a remote Linux box is still reached via OSC 777, never via
// the remote's desktop daemon.
func TestNotifyLinuxOverSSHUsesOSC777(t *testing.T) {
	defer restore()()
	goos = "linux"
	getenv = func(key string) string {
		if key == "TERM" {
			return "xterm-ghostty"
		}
		return ""
	}
	rec := &cmdRecorder{known: map[string]bool{"notify-send": true}}
	rec.install()
	var emitted []string
	emitSeq = func(seq string) error {
		emitted = append(emitted, seq)
		return nil
	}
	Notify("remote-build", "Waiting for your input")
	if len(emitted) != 1 || emitted[0] != "\x1b]777;notify;remote-build;Waiting for your input\a" {
		t.Fatalf("want one OSC 777 sequence, got %q", emitted)
	}
	if len(rec.called) != 0 {
		t.Fatalf("notify-send on the remote host should not run, got %v", rec.called)
	}
}

// Headless Linux and WSL have no notify-send; the bell is the floor.
func TestNotifyLinuxWithoutNotifySendRingsBell(t *testing.T) {
	defer restore()()
	goos = "linux"
	getenv = func(string) string { return "" }
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	var emitted []string
	emitSeq = func(seq string) error {
		emitted = append(emitted, seq)
		return nil
	}
	Notify("deploy", "Finished")
	if len(rec.called) != 0 {
		t.Fatalf("no command should run without notify-send, got %v", rec.called)
	}
	if len(emitted) != 1 || emitted[0] != "\a" {
		t.Fatalf("want one bell, got %q", emitted)
	}
}

func TestSanitizeSquashesControlCharacters(t *testing.T) {
	got := sanitize("line one\nline two\x1b]pwn\x07\ttab")
	if strings.ContainsAny(got, "\n\x1b\x07\t") {
		t.Fatalf("control characters should be gone, got %q", got)
	}
	if got != "line one line two ]pwn tab" {
		t.Fatalf("unexpected squash %q", got)
	}
}

// A semicolon in the title or body would read as an OSC 777 field
// separator and split the payload.
func TestOsc777StripsSemicolons(t *testing.T) {
	got := osc777("a;b", "c;d")
	if got != "\x1b]777;notify;a,b;c,d\a" {
		t.Fatalf("unexpected sequence %q", got)
	}
}
