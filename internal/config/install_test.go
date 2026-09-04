package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

// stubWSL puts the check in a WSL distro whose Windows drives are the
// given directories, so a table can build both sides of PATH on any OS.
func stubWSL(t *testing.T, windowsDirs ...string) {
	t.Helper()
	originalWSL, originalMount := isWSL, onWindowsMount
	isWSL = func() bool { return true }
	onWindowsMount = func(path string) (bool, error) {
		for _, dir := range windowsDirs {
			// The check resolves symlinks before asking, and a temp
			// directory on macOS sits behind one.
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return false, err
			}
			if strings.HasPrefix(path, resolved+string(filepath.Separator)) {
				return true, nil
			}
		}
		return false, nil
	}
	t.Cleanup(func() { isWSL, onWindowsMount = originalWSL, originalMount })
}

// installBinary drops an executable of the given name into a new
// directory and returns both, so a test can place it on PATH.
func installBinary(t *testing.T, name string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// Every built-in agent CLI is a bare command name, so each one resolves
// to a Windows install on the interop PATH the same way.
func TestCheckInstalledUnderWSLRejectsAWindowsOnlyInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	for _, tool := range []string{"claude", "codex", "opencode", "grok", "gemini", "hermes", "pi", "cmd"} {
		t.Run(tool, func(t *testing.T) {
			windows, path := installBinary(t, tool)
			stubWSL(t, windows)
			t.Setenv("PATH", windows)

			err := CheckInstalled(tool + " --resume")
			var missing MissingToolError
			if !errors.As(err, &missing) {
				t.Fatalf("got %v, want the Windows install reported missing", err)
			}
			if missing.Binary != tool || missing.WindowsPath != path {
				t.Fatalf("missing = %+v, want binary %q at %q", missing, tool, path)
			}
			if !strings.Contains(err.Error(), "WSL") || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %q, want the Windows path and the distro named", err)
			}
			if !strings.Contains(err.Error(), "install it with") {
				t.Fatalf("error = %q, want the install command", err)
			}
		})
	}
}

// The Windows PATH is appended after the distro's own, so the ordinary
// case is a Linux install found first; a user who reorders PATH must
// still launch the one inside the distro.
func TestCheckInstalledUnderWSLAcceptsADistroInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	windows, _ := installBinary(t, "claude")
	distro, _ := installBinary(t, "claude")
	stubWSL(t, windows)
	separator := string(os.PathListSeparator)
	for _, order := range []string{distro + separator + windows, windows + separator + distro} {
		t.Setenv("PATH", order)
		if err := CheckInstalled("claude"); err != nil {
			t.Fatalf("PATH %q: %v", order, err)
		}
	}
}

// An empty PATH entry means the working directory, so a distro copy
// sitting there answers for a Windows hit earlier on PATH.
func TestCheckInstalledUnderWSLAcceptsADistroInstallInAnEmptyPathEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	windows, _ := installBinary(t, "claude")
	distro, _ := installBinary(t, "claude")
	stubWSL(t, windows)
	t.Chdir(distro)
	t.Setenv("PATH", windows+string(os.PathListSeparator))

	if err := CheckInstalled("claude"); err != nil {
		t.Fatalf("a copy in the working directory should answer: %v", err)
	}
}

// Off WSL a Windows path is just a path, and the mount table is never
// consulted.
func TestCheckInstalledOutsideWSLKeepsTheFirstHit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	windows, _ := installBinary(t, "claude")
	originalWSL, originalMount := isWSL, onWindowsMount
	isWSL = func() bool { return false }
	onWindowsMount = func(string) (bool, error) {
		t.Fatal("the mount table has no bearing outside WSL")
		return false, nil
	}
	t.Cleanup(func() { isWSL, onWindowsMount = originalWSL, originalMount })
	t.Setenv("PATH", windows)

	if err := CheckInstalled("claude"); err != nil {
		t.Fatal(err)
	}
}

// A command naming a path asked for that exact file, so a tool block
// pointed at a Windows drive launches what it names.
func TestCheckInstalledUnderWSLKeepsAnExplicitPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	windows, path := installBinary(t, "claude")
	stubWSL(t, windows)

	if err := CheckInstalled(path + " --resume"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckInstalledKeepsMountLookupErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	windows, _ := installBinary(t, "claude")
	originalWSL, originalMount := isWSL, onWindowsMount
	unreadable := errors.New("mountinfo: permission denied")
	isWSL = func() bool { return true }
	onWindowsMount = func(string) (bool, error) { return false, unreadable }
	t.Cleanup(func() { isWSL, onWindowsMount = originalWSL, originalMount })
	t.Setenv("PATH", windows)

	err := CheckInstalled("claude")
	if !errors.Is(err, unreadable) {
		t.Fatalf("got %v, want the mount lookup error", err)
	}
	var missing MissingToolError
	if errors.As(err, &missing) {
		t.Fatal("an unreadable mount table must not read as a missing tool")
	}
}
