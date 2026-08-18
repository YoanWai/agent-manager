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
