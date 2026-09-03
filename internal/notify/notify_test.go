package notify

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func restore() func() {
	origGOOS, origEnv, origLook, origRun, origOutput, origRunEnv, origEmit := goos, getenv, lookPath, runCmd, runOutput, runEnv, emitSeq
	origDir, origWSL, origMac := configDir, isWSL, macPost
	return func() {
		goos, getenv, lookPath, runCmd, runOutput, runEnv, emitSeq = origGOOS, origEnv, origLook, origRun, origOutput, origRunEnv, origEmit
		configDir, isWSL, macPost = origDir, origWSL, origMac
	}
}

type cmdRecorder struct {
	known           map[string]bool
	called          [][]string
	envs            []map[string]string
	runErr          error
	outputByCommand map[string]string
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
	runOutput = func(_ time.Duration, name string, args ...string) (string, error) {
		r.called = append(r.called, append([]string{name}, args...))
		key := name
		if len(args) > 0 {
			key += " " + args[0]
		}
		if out, ok := r.outputByCommand[key]; ok {
			return out, nil
		}
		return "", r.runErr
	}
	runEnv = func(env map[string]string, name string, args ...string) error {
		r.called = append(r.called, append([]string{name}, args...))
		r.envs = append(r.envs, env)
		return r.runErr
	}
}

// plainMac makes the darwin branch behave as it does when the helper
// bundle cannot be used, so the AppleScript fallback is what runs.
func plainMac(t *testing.T) *cmdRecorder {
	t.Helper()
	goos = "darwin"
	getenv = func(string) string { return "" }
	macPost = func(string, string, string, string) error { return errors.New("no helper") }
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	return rec
}

func TestNotifyDarwinGhosttyUsesOSC777(t *testing.T) {
	defer restore()()
	rec := plainMac(t)
	getenv = func(key string) string {
		if key == "TERM_PROGRAM" {
			return "ghostty"
		}
		return ""
	}
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

func TestNotifyGhosttyFailureFallsBackToNative(t *testing.T) {
	defer restore()()
	rec := plainMac(t)
	getenv = func(key string) string {
		if key == "TERM_PROGRAM" {
			return "ghostty"
		}
		return ""
	}
	emitSeq = func(string) error { return errors.New("closed terminal") }
	Notify(Event{Session: "deploy", Tool: "claude", Kind: Waiting})
	if len(rec.called) != 1 || rec.called[0][0] != "osascript" {
		t.Fatalf("failed terminal delivery should fall back to the OS, got %v", rec.called)
	}
}

// The helper bundle owns the banner on macOS: the manager's name and icon,
// and a click that reveals the terminal. Nothing else runs when it posts.
func TestNotifyDarwinPostsThroughHelper(t *testing.T) {
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
			rec := plainMac(t)
			var posted []string
			macPost = func(sessionID, subtitle, body, sound string) error {
				posted = []string{sessionID, subtitle, body, sound}
				return nil
			}
			emitSeq = func(string) error { return nil }
			Notify(Event{ID: "sess-1", Session: "deploy", Tool: "codex", Kind: test.kind})
			want := []string{"sess-1", "deploy · codex", test.body, test.sound}
			if !slices.Equal(posted, want) {
				t.Fatalf("helper fields = %v, want %v", posted, want)
			}
			if len(rec.called) != 0 {
				t.Fatalf("nothing else should run once the helper posted, got %v", rec.called)
			}
		})
	}
}

func TestNotifyDarwinHelperFailureFallsBackToAppleScript(t *testing.T) {
	defer restore()()
	rec := plainMac(t)
	emitSeq = func(string) error { return nil }
	Notify(Event{Session: "deploy", Tool: "codex", Kind: Errored})
	if len(rec.called) != 1 || rec.called[0][0] != "osascript" {
		t.Fatalf("want one osascript call, got %v", rec.called)
	}
	call := rec.called[0]
	want := []string{"agent-manager", "deploy · codex", "✕ Errored", "Basso"}
	if len(call) < len(want) || !slices.Equal(call[len(call)-len(want):], want) {
		t.Fatalf("notification fields should ride argv unquoted, got %v", call)
	}
}

// A user who refused the manager's notifications in System Settings gets
// the bell, not a banner smuggled through another app.
func TestNotifyDarwinDeniedRingsBellOnly(t *testing.T) {
	defer restore()()
	rec := plainMac(t)
	macPost = func(string, string, string, string) error { return errDenied }
	var emitted []string
	emitSeq = func(seq string) error {
		emitted = append(emitted, seq)
		return nil
	}
	Notify(Event{Session: "deploy", Tool: "codex", Kind: Waiting})
	if len(rec.called) != 0 {
		t.Fatalf("no command should run after a refusal, got %v", rec.called)
	}
	if !slices.Equal(emitted, []string{"\a"}) {
		t.Fatalf("want one bell, got %q", emitted)
	}
}

func linuxDesktop(t *testing.T) *cmdRecorder {
	t.Helper()
	goos = "linux"
	getenv = func(string) string { return "" }
	isWSL = func() bool { return false }
	rec := &cmdRecorder{known: map[string]bool{"notify-send": true}, outputByCommand: map[string]string{}}
	rec.install()
	return rec
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
			rec := linuxDesktop(t)
			rec.outputByCommand["notify-send --help"] = "Usage: notify-send [OPTION…] <SUMMARY> [BODY]"
			emitSeq = func(string) error { return nil }
			Notify(Event{Session: "--help", Tool: "claude", Kind: test.kind})
			if len(rec.called) != 2 {
				t.Fatalf("want the probe and one notify-send call, got %v", rec.called)
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
			if !slices.Equal(rec.called[1], want) {
				t.Fatalf("unexpected notify-send args %v", rec.called[1])
			}
		})
	}
}

// A notify-send that reports actions stays up for the click; the click
// raises the terminal window and leaves the session for the manager.
func TestNotifyLinuxClickRaisesTerminalAndSelectsSession(t *testing.T) {
	defer restore()()
	rec := linuxDesktop(t)
	rec.known["xdotool"] = true
	rec.outputByCommand["notify-send --help"] = "  -A, --action=[NAME=]Text  Specifies the actions to display"
	rec.outputByCommand["notify-send --app-name=agent-manager"] = "default\n"
	getenv = func(key string) string {
		if key == "WINDOWID" {
			return "4194305"
		}
		return ""
	}
	dir := t.TempDir()
	configDir = func() (string, error) { return dir, nil }
	emitSeq = func(string) error { return nil }
	Notify(Event{ID: "sess-9", Session: "deploy", Tool: "claude", Kind: Waiting})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if id, ok := TakeFocus(dir); ok {
			if id != "sess-9" {
				t.Fatalf("focus request = %q, want sess-9", id)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the click never left a focus request")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var send, raise []string
	for _, call := range rec.called {
		switch {
		case call[0] == "notify-send" && len(call) > 1 && call[1] != "--help":
			send = call
		case call[0] == "xdotool":
			raise = call
		}
	}
	if !slices.Contains(send, "--action=default=Open") {
		t.Fatalf("notify-send should carry the default action, got %v", send)
	}
	if !slices.Equal(raise, []string{"xdotool", "windowactivate", "--sync", "4194305"}) {
		t.Fatalf("the click should raise the terminal window, got %v", raise)
	}
}

// Over plain SSH only TERM crosses to the remote host, so a Ghostty user
// working on a remote Linux box is still reached via OSC 777, never via
// the remote's desktop daemon.
func TestNotifyLinuxOverSSHUsesOSC777(t *testing.T) {
	defer restore()()
	rec := linuxDesktop(t)
	getenv = func(key string) string {
		if key == "TERM" {
			return "xterm-ghostty"
		}
		return ""
	}
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

// WSL has no notification daemon of its own; the banner is a Windows toast
// posted through the interop PowerShell, with the text carried in the
// environment rather than the command line.
func TestNotifyWSLPostsWindowsToast(t *testing.T) {
	defer restore()()
	goos = "linux"
	getenv = func(string) string { return "" }
	isWSL = func() bool { return true }
	rec := &cmdRecorder{known: map[string]bool{"powershell.exe": true}}
	rec.install()
	emitSeq = func(string) error { return nil }
	Notify(Event{Session: "deploy", Tool: "claude", Kind: Errored})
	if len(rec.called) != 1 || !strings.HasSuffix(rec.called[0][0], "/powershell.exe") {
		t.Fatalf("want one powershell call, got %v", rec.called)
	}
	call := rec.called[0]
	if !slices.Equal(call[1:4], []string{"-NoProfile", "-NonInteractive", "-EncodedCommand"}) || len(call) != 5 {
		t.Fatalf("unexpected powershell invocation %v", call)
	}
	if call[4] != encodedCommand(toastScript) {
		t.Fatal("the encoded command should be the toast script")
	}
	env := rec.envs[0]
	want := map[string]string{
		"AM_TOAST_TITLE":    "agent-manager",
		"AM_TOAST_SUBTITLE": "deploy · claude",
		"AM_TOAST_BODY":     "✕ Errored",
		"AM_TOAST_SOUND":    "ms-winsoundevent:Notification.Looping.Alarm2",
		"AM_TOAST_APPID":    toastAppID,
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("%s = %q, want %q", key, env[key], value)
		}
	}
}

func TestNotifyWSLWithoutPowerShellOnPathUsesTheWindowsCopy(t *testing.T) {
	defer restore()()
	goos = "linux"
	getenv = func(string) string { return "" }
	isWSL = func() bool { return true }
	rec := &cmdRecorder{known: map[string]bool{}}
	rec.install()
	emitSeq = func(string) error { return nil }
	Notify(Event{Session: "deploy", Tool: "claude", Kind: Waiting})
	if len(rec.called) != 1 || rec.called[0][0] != powershellFallback {
		t.Fatalf("want the System32 powershell, got %v", rec.called)
	}
}

// PowerShell reads -EncodedCommand as base64 over UTF-16LE.
func TestEncodedCommandIsUTF16LEBase64(t *testing.T) {
	got := encodedCommand("Ab€")
	if got != "QQBiAKwg" {
		t.Fatalf("encodedCommand = %q, want QQBiAKwg", got)
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
			rec := linuxDesktop(t)
			rec.known["notify-send"] = false
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
		name string
		goos string
		wsl  bool
	}{
		{"macOS", "darwin", false},
		{"Linux", "linux", false},
		{"WSL", "linux", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer restore()()
			goos = test.goos
			getenv = func(string) string { return "" }
			isWSL = func() bool { return test.wsl }
			macPost = func(string, string, string, string) error { return errors.New("no helper") }
			rec := &cmdRecorder{known: map[string]bool{"notify-send": true, "powershell.exe": true}, runErr: errors.New("desktop unavailable")}
			rec.install()
			var emitted []string
			emitSeq = func(seq string) error {
				emitted = append(emitted, seq)
				return nil
			}
			Notify(Event{Session: "deploy", Tool: "custom-cli", Kind: Errored})
			if !slices.Equal(emitted, []string{"\a"}) {
				t.Fatalf("failed native delivery should ring once, got %q", emitted)
			}
		})
	}
}

func TestFocusRequestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := TakeFocus(dir); ok {
		t.Fatal("nothing should be pending in an empty directory")
	}
	if err := RequestFocus(dir, "sess-3"); err != nil {
		t.Fatal(err)
	}
	id, ok := TakeFocus(dir)
	if !ok || id != "sess-3" {
		t.Fatalf("TakeFocus = %q, %v; want sess-3", id, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, focusFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a taken request must not be served twice")
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
	rec := plainMac(t)
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

// Two managers polling the same directory must never both act on one
// click. A click published while another is still pending replaces it,
// which is the intent: the newest click is the one the user meant.
func TestTakeFocusClaimsEachRequestOnce(t *testing.T) {
	dir := t.TempDir()
	if err := RequestFocus(dir, "sess-first"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	taken := map[string]int{}
	var writes, reads sync.WaitGroup
	for writer := 0; writer < 3; writer++ {
		writes.Add(1)
		go func(writer int) {
			defer writes.Done()
			for i := 0; i < 50; i++ {
				if err := RequestFocus(dir, "sess-"+strconv.Itoa(writer)+"-"+strconv.Itoa(i)); err != nil {
					t.Error(err)
					return
				}
			}
		}(writer)
	}
	for reader := 0; reader < 4; reader++ {
		reads.Add(1)
		go func() {
			defer reads.Done()
			for i := 0; i < 200; i++ {
				if id, ok := TakeFocus(dir); ok {
					mu.Lock()
					taken[id]++
					mu.Unlock()
				}
			}
		}()
	}
	writes.Wait()
	reads.Wait()
	if len(taken) == 0 {
		t.Fatal("no click was ever claimed")
	}
	for id, count := range taken {
		if id == "" {
			t.Fatal("a claim returned an empty session")
		}
		if count != 1 {
			t.Fatalf("%s was claimed %d times, want once", id, count)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != focusFile {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}
