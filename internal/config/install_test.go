package config

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestCheckInstalledAcceptsPresentBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("looks up a Unix shell")
	}
	if err := CheckInstalled("sh -c true"); err != nil {
		t.Fatalf("present binary: %v", err)
	}
}

func TestCheckInstalledSkipsEmptyCommand(t *testing.T) {
	if err := CheckInstalled(""); err != nil {
		t.Fatalf("empty command: %v", err)
	}
}

func TestCheckInstalledRejectsMissingBinary(t *testing.T) {
	err := CheckInstalled("am-missing-cli-xyz --flag")
	var missing MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("got %v", err)
	}
	if missing.Binary != "am-missing-cli-xyz" {
		t.Fatalf("binary = %q", missing.Binary)
	}
	if !strings.Contains(err.Error(), "install") {
		t.Fatalf("error should name how to install, got %q", err)
	}
}

func TestCheckInstalledNamesOfficialInstaller(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = orig })

	err := CheckInstalled("claude")
	var missing MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("got %v", err)
	}
	if missing.Binary != "claude" {
		t.Fatalf("binary = %q", missing.Binary)
	}
	if !strings.Contains(err.Error(), "claude.ai/install.sh") {
		t.Fatalf("error = %q", err)
	}
}

func TestCheckInstalledRejectsWindowsMountUnderWSL(t *testing.T) {
	origLookPath, origWSLDetect := lookPath, wslDetect
	lookPath = func(string) (string, error) {
		return "/mnt/c/Users/dev/AppData/Roaming/npm/claude", nil
	}
	wslDetect = func() bool { return true }
	t.Cleanup(func() { lookPath, wslDetect = origLookPath, origWSLDetect })

	err := CheckInstalled("claude")
	var missing MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("a Windows-mount hit under WSL should read as not installed in the distro, got %v", err)
	}
	if missing.Binary != "claude" {
		t.Fatalf("binary = %q", missing.Binary)
	}
}

func TestCheckInstalledAcceptsWindowsMountOutsideWSL(t *testing.T) {
	origLookPath, origWSLDetect := lookPath, wslDetect
	lookPath = func(string) (string, error) {
		return "/mnt/c/Users/dev/AppData/Roaming/npm/claude", nil
	}
	wslDetect = func() bool { return false }
	t.Cleanup(func() { lookPath, wslDetect = origLookPath, origWSLDetect })

	if err := CheckInstalled("claude"); err != nil {
		t.Fatalf("a /mnt/c path outside WSL is an ordinary mounted path, not a distro miss: %v", err)
	}
}

func TestCheckInstalledAcceptsDistroBinaryUnderWSL(t *testing.T) {
	origLookPath, origWSLDetect := lookPath, wslDetect
	lookPath = func(string) (string, error) { return "/usr/local/bin/claude", nil }
	wslDetect = func() bool { return true }
	t.Cleanup(func() { lookPath, wslDetect = origLookPath, origWSLDetect })

	if err := CheckInstalled("claude"); err != nil {
		t.Fatalf("a distro-installed binary under WSL must still pass: %v", err)
	}
}

func TestCheckInstalledKeepsLookupErrors(t *testing.T) {
	orig := lookPath
	denied := errors.New("permission denied")
	lookPath = func(string) (string, error) { return "", denied }
	t.Cleanup(func() { lookPath = orig })

	err := CheckInstalled("claude")
	if !errors.Is(err, denied) {
		t.Fatalf("got %v", err)
	}
	var missing MissingToolError
	if errors.As(err, &missing) {
		t.Fatalf("lookup errors must not become MissingToolError")
	}
}
