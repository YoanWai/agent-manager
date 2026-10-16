//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A TUI launched without a controlling terminal fails with the one message
// that tells the user what to open it in; the Setsid call is what makes the
// child terminal-less, which Windows has no equivalent of.
func TestMainReportsHeadlessStartupFailure(t *testing.T) {
	if prepareMainProcess() {
		main()
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
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
	if err == nil {
		t.Fatalf("agent-manager unexpectedly started without a controlling terminal:\n%s", out)
	}
	if !strings.Contains(string(out), "could not open a new TTY") {
		t.Fatalf("agent-manager reported the wrong startup failure:\n%s", out)
	}
}
