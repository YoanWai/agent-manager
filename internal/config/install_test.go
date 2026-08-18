package config

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestCheckInstalledAcceptsPresentBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("looks up a Unix shell")
	}
	if err := CheckInstalled(Tool{Command: "sh -c true"}); err != nil {
		t.Fatalf("present binary: %v", err)
	}
}

func TestCheckInstalledSkipsEmptyCommand(t *testing.T) {
	if err := CheckInstalled(Tool{Command: "", Shell: true}); err != nil {
		t.Fatalf("empty command: %v", err)
	}
}

func TestCheckInstalledRejectsMissingBinary(t *testing.T) {
	err := CheckInstalled(Tool{Command: "am-missing-cli-xyz --flag"})
	var missing MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("got %v", err)
	}
	if missing.Binary != "am-missing-cli-xyz" {
		t.Fatalf("binary = %q", missing.Binary)
	}
	if missing.Install != "" {
		t.Fatalf("unknown binary should not invent an installer, got %q", missing.Install)
	}
}

func TestCheckInstalledNamesOfficialInstaller(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() { lookPath = orig })

	err := CheckInstalled(Tool{Command: "claude"})
	var missing MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("got %v", err)
	}
	if missing.Binary != "claude" {
		t.Fatalf("binary = %q", missing.Binary)
	}
	if !strings.Contains(missing.Install, "claude.ai/install.sh") {
		t.Fatalf("install = %q", missing.Install)
	}
}

func TestOfficialInstallIsPortable(t *testing.T) {
	if len(officialInstall) == 0 {
		t.Fatal("no official installers")
	}
	for name, got := range officialInstall {
		if strings.Contains(got, "brew") || strings.Contains(got, "apt") || strings.Contains(got, "winget") {
			t.Errorf("%s install is OS-specific: %s", name, got)
		}
	}
}
