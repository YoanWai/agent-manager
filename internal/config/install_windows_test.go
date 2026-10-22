//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolveTo(t *testing.T, path string) {
	t.Helper()
	orig := lookPath
	lookPath = func(string) (string, error) { return path, nil }
	t.Cleanup(func() { lookPath = orig })
}

// cmd.exe parses the arguments of a batch file again, and PowerShell's
// escaped quote ends an argument there, so a .cmd or .bat with no .ps1
// beside it cannot take a prompt holding a double quote whole.
func TestCheckInstalledRefusesABatchFileAlone(t *testing.T) {
	for _, ext := range []string{".cmd", ".bat", ".CMD"} {
		shim := filepath.Join(t.TempDir(), "tool"+ext)
		touch(t, shim)
		resolveTo(t, shim)
		err := CheckInstalled("tool --flag")
		if err == nil || !strings.Contains(err.Error(), "batch file") || !strings.Contains(err.Error(), shim) {
			t.Fatalf("%s: got %v", ext, err)
		}
	}
}

// npm installs a .ps1 beside every .cmd, and PowerShell runs the script
// for a command naming no extension; one naming the .cmd runs the .cmd.
func TestCheckInstalledTrustsThePS1BesideABatchFile(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "tool.cmd")
	touch(t, shim)
	touch(t, filepath.Join(dir, "tool.ps1"))
	resolveTo(t, shim)
	for _, command := range []string{"tool", filepath.ToSlash(filepath.Join(dir, "tool"))} {
		if err := CheckInstalled(command + " --flag"); err != nil {
			t.Fatalf("%s with a .ps1 beside the .cmd: %v", command, err)
		}
	}
	for _, command := range []string{"tool.cmd", shim, filepath.ToSlash(shim)} {
		if err := CheckInstalled(command + " --flag"); err == nil {
			t.Fatalf("%s names the .cmd, which runs instead of the .ps1", command)
		}
	}
}

func TestCheckInstalledAcceptsAnExe(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "tool.exe")
	touch(t, exe)
	resolveTo(t, exe)
	if err := CheckInstalled("tool --flag"); err != nil {
		t.Fatalf("exe: %v", err)
	}
}
