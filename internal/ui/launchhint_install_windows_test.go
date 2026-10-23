//go:build windows

package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/pwsh"
)

// The line typed into the pane names the shell Path found and quotes the
// script path, so an install lands whatever the pane's shell is.
func TestInstallLineNamesTheShellAndQuotesTheScript(t *testing.T) {
	shell, err := pwsh.Path()
	if err != nil {
		t.Skip("PowerShell not found")
	}
	line, err := installLine(`C:\dir with space\install.ps1`)
	if err != nil {
		t.Fatalf("installLine: %v", err)
	}
	want := filepath.Base(shell) + ` -NoProfile -ExecutionPolicy Bypass -File "C:\dir with space\install.ps1"`
	if line != want {
		t.Fatalf("installLine = %q, want %q", line, want)
	}
}

// The script's status write cannot be skipped out of: an installer that
// exits still records its code, and one that throws records 127.
func TestInstallScriptRecordsExitAndCommandNotFound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		want    string
	}{
		{"exit code", "cmd /c exit 3", "3"},
		{"success", "cmd /c exit 0", "0"},
		{"command not found", "definitely-no-such-command-xyz", "127"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			statusFile := filepath.Join(dir, "status")
			script := filepath.Join(dir, "install.ps1")
			if err := os.WriteFile(script, pwsh.ScriptFile(installScript(tc.command, statusFile)), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := installLine(script); err != nil {
				t.Fatal(err)
			}
			// The pane's shell strips the quoting installLine adds; args
			// here match the line it would be left with.
			shell, _ := pwsh.Path()
			command := exec.Command(shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
			command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if out, err := command.CombinedOutput(); err != nil {
				t.Logf("install shell: %v: %s", err, out)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				status, readErr := os.ReadFile(statusFile)
				if readErr == nil {
					if got := strings.TrimSpace(string(status)); got != tc.want {
						t.Fatalf("status = %q, want %q", got, tc.want)
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatal("the install script never wrote its status")
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}
