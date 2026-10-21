package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeVersionCLI puts a binary named name on the lookup path that answers
// --version with banner, and counts how often it is asked.
func fakeVersionCLI(t *testing.T, name, banner string) (path string, asked *int) {
	t.Helper()
	path = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(banner), 0o755); err != nil {
		t.Fatal(err)
	}
	asked = new(int)
	origLook, origRead := lookPath, readVersionOutput
	lookPath = func(binary string) (string, error) {
		if binary != name {
			return "", errors.New("unexpected lookup of " + binary)
		}
		return path, nil
	}
	readVersionOutput = func(target string) ([]byte, error) {
		*asked++
		return os.ReadFile(target)
	}
	t.Cleanup(func() { lookPath, readVersionOutput = origLook, origRead })
	return path, asked
}

func TestCheckVersionRefusesAnOlderRelease(t *testing.T) {
	fakeVersionCLI(t, "pi", "0.84.2\n")

	err := CheckVersion(Tool{Command: "pi --mode rpc", MinimumVersion: "0.84.3"})
	var outdated OutdatedToolError
	if !errors.As(err, &outdated) {
		t.Fatalf("err = %v, want OutdatedToolError", err)
	}
	if outdated != (OutdatedToolError{Binary: "pi", Installed: "0.84.2", Minimum: "0.84.3"}) {
		t.Fatalf("outdated = %+v", outdated)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "update it") {
		t.Fatalf("error should say how to fix it, got %q", err)
	}
}

func TestCheckVersionAcceptsTheMinimumAndNewer(t *testing.T) {
	for _, banner := range []string{"0.84.3\n", "1.3.0\n"} {
		fakeVersionCLI(t, "pi", banner)
		if err := CheckVersion(Tool{Command: "pi", MinimumVersion: "0.84.3"}); err != nil {
			t.Fatalf("%q: %v", banner, err)
		}
	}
}

func TestCheckVersionSkipsToolsWithoutAMinimum(t *testing.T) {
	_, asked := fakeVersionCLI(t, "claude", "2.1.304 (Claude Code)\n")
	if err := CheckVersion(Tool{Command: "claude"}); err != nil {
		t.Fatal(err)
	}
	if *asked != 0 {
		t.Fatalf("asked %d times, want no probe without a minimum", *asked)
	}
}

func TestCheckVersionAsksEachBuildOnce(t *testing.T) {
	path, asked := fakeVersionCLI(t, "pi", "0.84.3\n")
	tool := Tool{Command: "pi", MinimumVersion: "0.84.3"}
	for range 3 {
		if err := CheckVersion(tool); err != nil {
			t.Fatal(err)
		}
	}
	if *asked != 1 {
		t.Fatalf("asked %d times, want 1", *asked)
	}
	if err := os.WriteFile(path, []byte("0.84.10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CheckVersion(tool); err != nil {
		t.Fatal(err)
	}
	if *asked != 2 {
		t.Fatalf("asked %d times after the binary changed, want 2", *asked)
	}
}

func TestCheckVersionReportsAMissingCLIAsNotInstalled(t *testing.T) {
	origLook, origRead := lookPath, readVersionOutput
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	asked := 0
	readVersionOutput = func(string) ([]byte, error) {
		asked++
		return nil, errors.New("probed")
	}
	t.Cleanup(func() { lookPath, readVersionOutput = origLook, origRead })

	err := CheckVersion(Tool{Command: "pi", MinimumVersion: "0.84.3"})
	var missing MissingToolError
	if !errors.As(err, &missing) || missing.Binary != "pi" || missing.WindowsPath != "" {
		t.Fatalf("err = %v, want pi missing in this distro", err)
	}
	if asked != 0 {
		t.Fatalf("--version ran %d times on a missing binary", asked)
	}
}

func TestCheckVersionDoesNotReadAWindowsOnlyCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds a Unix PATH")
	}
	windows, path := installBinary(t, "pi")
	stubWSL(t, windows)
	t.Setenv("PATH", windows)
	origRead := readVersionOutput
	asked := 0
	readVersionOutput = func(string) ([]byte, error) {
		asked++
		return []byte("0.1.0\n"), nil
	}
	t.Cleanup(func() { readVersionOutput = origRead })

	err := CheckVersion(Tool{Command: "pi", MinimumVersion: "0.84.3"})
	var missing MissingToolError
	if !errors.As(err, &missing) || missing.Binary != "pi" || missing.WindowsPath != path {
		t.Fatalf("err = %v, want the Windows copy at %s", err, path)
	}
	if asked != 0 {
		t.Fatalf("--version ran %d times on a Windows-only CLI", asked)
	}
}

func TestCheckVersionNamesABannerWithoutAVersion(t *testing.T) {
	fakeVersionCLI(t, "pi", "pi dev build\n")
	err := CheckVersion(Tool{Command: "pi", MinimumVersion: "0.84.3"})
	if err == nil || !strings.Contains(err.Error(), "pi dev build") {
		t.Fatalf("err = %v, want the banner quoted", err)
	}
}

func TestVersionNumberReadsEachCLIBanner(t *testing.T) {
	for banner, want := range map[string]string{
		"2.1.304 (Claude Code)":             "2.1.304",
		"codex-cli 0.163.0":                 "0.163.0",
		"1.19.0":                            "1.19.0",
		"grok 1.0.62 (1474f9cbdd91)":        "1.0.62",
		"omp/18.8.8":                        "18.8.8",
		"Muse Code 1.4.11 (1.4.11-R6667.1)": "1.4.11",
	} {
		if got := versionNumber.FindString(banner); got != want {
			t.Errorf("%q read as %q, want %q", banner, got, want)
		}
	}
}

func TestBuiltinMinimumVersionsParse(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for name, tool := range cfg.Tools {
		if tool.MinimumVersion != "" && !versionNumber.MatchString(tool.MinimumVersion) {
			t.Errorf("%s minimum_version %q is not a version", name, tool.MinimumVersion)
		}
	}
	if cfg.Tools["pi"].MinimumVersion != "0.84.3" {
		t.Fatalf("pi minimum_version = %q", cfg.Tools["pi"].MinimumVersion)
	}
}

func TestConcurrentVersionChecksShareTheProbe(t *testing.T) {
	_, asked := fakeVersionCLI(t, "pi", "1.0.0")
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if err := CheckVersion(Tool{Command: "pi", MinimumVersion: "0.84.3"}); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if *asked != 1 {
		t.Fatalf("probed %d times, want 1", *asked)
	}
}

func TestVersionProbeReturnsWhenAChildKeepsStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe script needs a POSIX shell")
	}
	script := filepath.Join(t.TempDir(), "slow-cli")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '1.2.3\\n'\nsleep 30 &\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := readVersionOutput(script)
	if err != nil || versionNumber.FindString(string(out)) != "1.2.3" {
		t.Fatalf("probe = %q, %v", out, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("probe waited %s on the child's stdout", time.Since(start))
	}
}
