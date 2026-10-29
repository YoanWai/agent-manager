//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
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

// A process a managed pane started speaks for that pane's session, whatever
// session its variable names.
func TestCallerSessionTrustsThePaneThatStartedTheProcess(t *testing.T) {
	if out := os.Getenv("AGENT_MANAGER_CALLER_OUT"); out != "" {
		if err := os.WriteFile(out+".part", []byte(callerSession(t.TempDir())), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(out+".part", out); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	isolateTmux(t)
	driver, err := tmux.New()
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	out := filepath.Join(t.TempDir(), "caller")
	command := "AGENT_MANAGER_CALLER_OUT=" + tmux.ShellQuote(out) + " " + hooks.EnvSessionID + "=deadbeef " +
		tmux.ShellQuote(os.Args[0]) + " -test.run='^" + t.Name() + "$'; sleep 60"
	id := "ca11ab1e"
	if err := driver.Create(id, os.TempDir(), command, nil, 80, 24); err != nil {
		t.Fatalf("create the pane: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	var got []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if got, err = os.ReadFile(out); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the process in the pane reported no caller: %v", err)
	}
	if string(got) != id {
		t.Fatalf("callerSession in the pane with another session's variable = %q, want %q", got, id)
	}
}
