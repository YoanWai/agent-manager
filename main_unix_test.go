//go:build !windows

package main

import (
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agent-manager with no terminal: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Usage: agent-manager [command]") {
		t.Fatalf("agent-manager did not print usage:\n%s", out)
	}
}
