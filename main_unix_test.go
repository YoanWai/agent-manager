//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Piped output is not a terminal, and Setsid drops the controlling
// terminal that a TUI would open. A no-argument start prints usage and
// exits 0 instead of failing that open, whether or not tmux is installed.
func TestMainHeadlessNoArgsPrintsUsage(t *testing.T) {
	if prepareMainProcess() {
		main()
		return
	}
	root, err := os.MkdirTemp("/tmp", "ammain")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove startup test directory: %v", err)
		}
	})
	for _, dir := range []string{"home", "config", "tmux"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cmd := mainTestCommand(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = replaceEnv(cmd.Env,
		"HOME", filepath.Join(root, "home"),
		"XDG_CONFIG_HOME", filepath.Join(root, "config"),
		"TMUX_TMPDIR", filepath.Join(root, "tmux"),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("agent-manager with no terminal: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: agent-manager [command]") {
		t.Fatalf("agent-manager did not print usage:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), noTerminalNotice) {
		t.Fatalf("stdout included the terminal notice:\n%s", stdout.String())
	}
	if stderr.String() != noTerminalNotice+"\n" {
		t.Fatalf("stderr = %q, want the terminal notice", stderr.String())
	}
}
