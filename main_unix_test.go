//go:build !windows

package main

import (
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

// A process a managed pane started speaks for that pane's session, whatever
// session its variable names.
func TestCallerSessionTrustsThePaneThatStartedTheProcess(t *testing.T) {
	if out := os.Getenv("AGENT_MANAGER_CALLER_OUT"); out != "" {
		if err := os.WriteFile(out+".part", []byte(callerSession()), 0o644); err != nil {
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
