package launchhint

import (
	"strings"
	"testing"
)

func TestMissingTextNamesAWindowsOnlyInstall(t *testing.T) {
	text := MissingText(Tool{Binary: "pi", WindowsPath: `C:\tools\pi.exe`}, "install it with: npm install -g pi")
	for _, want := range []string{"pi is installed on Windows, not in this WSL distro.", `C:\tools\pi.exe`, "npm install -g pi"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing text %q does not name %q", text, want)
		}
	}
	if strings.Contains(text, "is not installed") {
		t.Fatalf("text claims nothing is installed: %q", text)
	}
}

func TestMissingTextNamesTheCommandThatUnblocks(t *testing.T) {
	text := MissingText(Tool{Binary: "claude"}, "install it with: npm install -g @anthropic-ai/claude-code")
	if !strings.HasPrefix(text, "claude is not installed.") {
		t.Fatalf("text does not open on the reason: %q", text)
	}
	if !strings.Contains(text, "npm install -g @anthropic-ai/claude-code") {
		t.Fatalf("text does not carry the command: %q", text)
	}
}

func TestOutdatedTextAsksForTheUpdateWhenThereIsNoRecipe(t *testing.T) {
	text := OutdatedText(Tool{Binary: "pi", Installed: "0.84.2", Minimum: "0.90.0"}, "")
	if !strings.Contains(text, "pi 0.84.2 is older than 0.90.0") {
		t.Fatalf("text does not name the gap: %q", text)
	}
	if !strings.Contains(text, "Update it the way you installed it") {
		t.Fatalf("text does not ask for the update: %q", text)
	}
}

func TestOutdatedTextNamesTheInstaller(t *testing.T) {
	text := OutdatedText(Tool{Binary: "claude", Installed: "1.0.0", Minimum: "2.0.0"}, "npm install -g @anthropic-ai/claude-code")
	if !strings.Contains(text, "Update it with: npm install -g @anthropic-ai/claude-code") {
		t.Fatalf("text does not name the installer: %q", text)
	}
}

func TestHermesTextNamesThePipCommandAndItsAbsence(t *testing.T) {
	if text := HermesText(""); !strings.Contains(text, "Install the mcp package into the Python that runs Hermes") {
		t.Fatalf("text without an interpreter does not say what to install: %q", text)
	}
	text := HermesText("pip install mcp")
	if !strings.Contains(text, "Run `pip install mcp`") {
		t.Fatalf("text does not name the pip line: %q", text)
	}
}

func TestReadStatus(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  string
		code  string
		state SettleState
	}{
		{name: "an empty file", data: "", state: Running},
		{name: "whitespace only", data: " \n", state: Running},
		{name: "a clean exit", data: "0\n", code: "0", state: Ready},
		{name: "a failure", data: "127", code: "127", state: Exited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, state := ReadStatus([]byte(tc.data))
			if code != tc.code || state != tc.state {
				t.Fatalf("ReadStatus(%q) = %q, %v; want %q, %v", tc.data, code, state, tc.code, tc.state)
			}
		})
	}
}

func TestSettleMessage(t *testing.T) {
	text, failed := SettleMessage(Exited, "pi", "install-pi", "127")
	if !failed || !strings.Contains(text, "status 127") || !strings.Contains(text, "install-pi") {
		t.Fatalf("exited = %q, %v; want a failure naming the status and the shell", text, failed)
	}
	text, failed = SettleMessage(BinaryMissing, "pi", "install-pi", "0")
	if !failed || !strings.Contains(text, "pi is still not on PATH") {
		t.Fatalf("binary missing = %q, %v", text, failed)
	}
	text, failed = SettleMessage(Ready, "pi", "install-pi", "0")
	if failed || text != "pi installed" {
		t.Fatalf("ready = %q, %v", text, failed)
	}
	if text, failed = SettleMessage(ShellGone, "pi", "install-pi", ""); text != "" || failed {
		t.Fatalf("shell gone = %q, %v; want no message", text, failed)
	}
}
