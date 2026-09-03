package notify

import (
	"slices"
	"strings"
	"testing"
)

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
