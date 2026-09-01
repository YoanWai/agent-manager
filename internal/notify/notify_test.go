package notify

import (
	"errors"
	"slices"
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
	runErr error
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
		return r.runErr
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
	Notify(Event{Session: "deploy", Tool: "claude", Kind: Waiting})
	if len(emitted) != 1 || emitted[0] != "\x1b]777;notify;agent-manager;◆ Waiting for your input — deploy · claude\a" {
		t.Fatalf("want one OSC 777 sequence, got %q", emitted)
	}
	if len(rec.called) != 0 {
		t.Fatalf("osascript should not run inside Ghostty, got %v", rec.called)
	}
}

// iTerm2 names itself in TERM_PROGRAM at the local shell; through tmux and
// over SSH only LC_TERMINAL survives, so both markers must reach OSC 9.
func TestNotifyITermUsesOSC9(t *testing.T) {
	for _, test := range []struct {
		name string
		goos string
		env  map[string]string
	}{
		{"local", "darwin", map[string]string{"TERM_PROGRAM": "iTerm.app", "LC_TERMINAL": "iTerm2"}},
		{"tmux", "darwin", map[string]string{"TERM_PROGRAM": "tmux", "LC_TERMINAL": "iTerm2"}},
		{"ssh", "linux", map[string]string{"TERM": "xterm-256color", "LC_TERMINAL": "iTerm2"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer restore()()
			goos = test.goos
			getenv = func(key string) string { return test.env[key] }
			rec := &cmdRecorder{known: map[string]bool{"notify-send": true}}
			rec.install()
			var emitted []string
			emitSeq = func(seq string) error {
				emitted = append(emitted, seq)
				return nil
			}
			Notify(Event{Session: "deploy", Tool: "claude", Kind: Finished})
			if len(emitted) != 1 || emitted[0] != "\x1b]9;● Finished — deploy · claude\a" {
				t.Fatalf("want one OSC 9 sequence, got %q", emitted)
			}
			if len(rec.called) != 0 {
				t.Fatalf("no desktop command should run inside iTerm2, got %v", rec.called)
			}
		})
	}
}

func TestNotifyITermFailureFallsBackToNative(t *testing.T) {
	defer restore()()
	goos = "darwin"
	getenv = func(key string) string {
		if key == "TERM_PROGRAM" {
			return "iTerm.app"
		}
		return ""
	}
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	emitSeq = func(string) error { return errors.New("closed terminal") }
	Notify(Event{Session: "deploy", Tool: "claude", Kind: Waiting})
	if len(rec.called) != 1 || rec.called[0][0] != "osascript" {
		t.Fatalf("failed terminal delivery should fall back to the OS, got %v", rec.called)
	}
}

func TestNotifyGhosttyFailureFallsBackToNative(t *testing.T) {
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
	emitSeq = func(string) error { return errors.New("closed terminal") }
	Notify(Event{Session: "deploy", Tool: "claude", Kind: Waiting})
	if len(rec.called) != 1 || rec.called[0][0] != "osascript" {
		t.Fatalf("failed terminal delivery should fall back to the OS, got %v", rec.called)
	}
}

func TestNotifyDarwinPlainTerminalUsesAppleScript(t *testing.T) {
	tests := []struct {
		kind  Kind
		body  string
		sound string
	}{
		{Waiting, "◆ Waiting for your input", "Funk"},
		{Finished, "● Finished", "Hero"},
		{Errored, "✕ Errored", "Basso"},
	}
	for _, test := range tests {
		t.Run(test.sound, func(t *testing.T) {
			defer restore()()
			goos = "darwin"
			getenv = func(string) string { return "" }
			rec := &cmdRecorder{known: map[string]bool{}}
			rec.install()
			emitSeq = func(string) error { return nil }
			Notify(Event{Session: "deploy", Tool: "codex", Kind: test.kind})
			if len(rec.called) != 1 || rec.called[0][0] != "osascript" {
				t.Fatalf("want one osascript call, got %v", rec.called)
			}
			call := rec.called[0]
			want := []string{"agent-manager", "deploy · codex", test.body, test.sound}
			if len(call) < len(want) || !slices.Equal(call[len(call)-len(want):], want) {
				t.Fatalf("notification fields should ride argv unquoted, got %v", call)
			}
		})
	}
}

func TestNotifyLinuxUsesPortableStatusHints(t *testing.T) {
	tests := []struct {
		kind     Kind
		body     string
		sound    string
		urgency  string
		icon     string
		category string
	}{
		{Waiting, "◆ Waiting for your input", "dialog-question", "normal", "dialog-question", "x-agent-manager.session.waiting"},
		{Finished, "● Finished", "complete-download", "low", "emblem-default", "x-agent-manager.session.finished"},
		{Errored, "✕ Errored", "dialog-error", "critical", "dialog-error", "x-agent-manager.session.errored"},
	}
	for _, test := range tests {
		t.Run(test.urgency, func(t *testing.T) {
			defer restore()()
			goos = "linux"
			getenv = func(string) string { return "" }
			rec := &cmdRecorder{known: map[string]bool{"notify-send": true}}
			rec.install()
			emitSeq = func(string) error { return nil }
			Notify(Event{Session: "--help", Tool: "claude", Kind: test.kind})
			if len(rec.called) != 1 {
				t.Fatalf("want one notify-send call, got %v", rec.called)
			}
			want := []string{
				"notify-send",
				"--app-name=agent-manager",
				"--urgency=" + test.urgency,
				"--category=" + test.category,
				"--icon=" + test.icon,
				"--hint=string:sound-name:" + test.sound,
				"--", "agent-manager", test.body + " — --help · claude",
			}
			if !slices.Equal(rec.called[0], want) {
				t.Fatalf("unexpected notify-send args %v", rec.called[0])
			}
		})
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
	Notify(Event{Session: "remote-build", Tool: "codex", Kind: Waiting})
	if len(emitted) != 1 || emitted[0] != "\x1b]777;notify;agent-manager;◆ Waiting for your input — remote-build · codex\a" {
		t.Fatalf("want one OSC 777 sequence, got %q", emitted)
	}
	if len(rec.called) != 0 {
		t.Fatalf("notify-send on the remote host should not run, got %v", rec.called)
	}
}

// Headless Linux and WSL have no notify-send; the bell is the floor.
func TestNotifyLinuxWithoutNotifySendRingsBell(t *testing.T) {
	for _, test := range []struct {
		name string
		kind Kind
	}{
		{"waiting", Waiting},
		{"finished", Finished},
		{"errored", Errored},
	} {
		t.Run(test.name, func(t *testing.T) {
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
			Notify(Event{Session: "deploy", Tool: "custom-cli", Kind: test.kind})
			if len(rec.called) != 0 {
				t.Fatalf("no command should run without notify-send, got %v", rec.called)
			}
			if len(emitted) != 1 || emitted[0] != "\a" {
				t.Fatalf("want one bell, got %q", emitted)
			}
		})
	}
}

func TestNotifyNativeFailureRingsBell(t *testing.T) {
	for _, test := range []struct {
		name  string
		goos  string
		known map[string]bool
	}{
		{"macOS", "darwin", map[string]bool{}},
		{"Linux", "linux", map[string]bool{"notify-send": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer restore()()
			goos = test.goos
			getenv = func(string) string { return "" }
			rec := &cmdRecorder{known: test.known, runErr: errors.New("desktop unavailable")}
			rec.install()
			var emitted []string
			emitSeq = func(seq string) error {
				emitted = append(emitted, seq)
				return nil
			}
			Notify(Event{Session: "deploy", Tool: "custom-cli", Kind: Errored})
			if len(rec.called) != 1 {
				t.Fatalf("want one native attempt, got %v", rec.called)
			}
			if !slices.Equal(emitted, []string{"\a"}) {
				t.Fatalf("failed native delivery should ring once, got %q", emitted)
			}
		})
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

func TestNotifyIgnoresUnknownKind(t *testing.T) {
	defer restore()()
	goos = "darwin"
	getenv = func(string) string { return "" }
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	var emitted []string
	emitSeq = func(seq string) error {
		emitted = append(emitted, seq)
		return nil
	}
	Notify(Event{Session: "deploy"})
	if len(rec.called) != 0 || len(emitted) != 0 {
		t.Fatalf("unknown transition should stay quiet, commands=%v escapes=%q", rec.called, emitted)
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
