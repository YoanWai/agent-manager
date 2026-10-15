//go:build windows

package notify

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/pwsh"
)

// The command reads the event from its environment under PowerShell too,
// and agent-written text in it is data, never a subexpression.
func TestRunCommandPassesTheEventThroughTheEnvironment(t *testing.T) {
	dir, out := shellOutFile(t)
	pwned := filepath.Join(dir, "pwned")
	event := Event{ID: "sess-1", Session: "$(New-Item " + pwned + ")", Tool: "codex", Kind: Waiting, Dir: dir}
	RunCommand(`[IO.File]::WriteAllText(`+pwsh.Quote(out)+`, "$env:AM_NOTIFY_KIND|$env:AM_SESSION_NAME|$env:AM_CWD|$env:AM_BODY")`, dir, event)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the command did not run: %v", err)
	}
	if want := "waiting|$(New-Item " + pwned + ")|" + dir + "|◆ Waiting for your input"; string(got) != want {
		t.Fatalf("command saw %q, want %q", got, want)
	}
	if _, err := os.Stat(pwned); err == nil {
		t.Fatal("text from the event was run by the shell")
	}
	if _, err := os.Stat(filepath.Join(dir, CommandLog)); err == nil {
		t.Fatal("a command that succeeded should log nothing")
	}
}

func TestRunCommandLogsAFailureWithItsOutput(t *testing.T) {
	dir, _ := shellOutFile(t)
	RunCommand(`[Console]::Error.WriteLine("no route to ntfy"); exit 3`, dir, Event{ID: "sess-1", Session: "s", Kind: Errored})
	log, err := os.ReadFile(filepath.Join(dir, CommandLog))
	if err != nil {
		t.Fatalf("a failure should be logged: %v", err)
	}
	for _, want := range []string{"errored", "sess-1", "exit status 3", "no route to ntfy"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("log %q does not name %q", log, want)
		}
	}
}

// The timeout stops what the command started, not only PowerShell.
func TestRunCommandStopsTheCommandsChildrenAtTheTimeout(t *testing.T) {
	defer func(previous time.Duration) { commandTimeout = previous }(commandTimeout)
	commandTimeout = 3 * time.Second
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	RunCommand(`$child = Start-Process -PassThru -WindowStyle Hidden ping -ArgumentList '-n','60','127.0.0.1'; `+
		`[IO.File]::WriteAllText(`+pwsh.Quote(pidFile)+`, [string]$child.Id); Wait-Process -Id $child.Id`,
		dir, Event{ID: "sess-1", Session: "s", Kind: Waiting})
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the child never started: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	exited := make(chan struct{})
	go func() {
		child.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		child.Kill()
		t.Fatalf("child %d outlived the command's timeout", pid)
	}
	if log, err := os.ReadFile(filepath.Join(dir, CommandLog)); err != nil || !strings.Contains(string(log), "sess-1") {
		t.Fatalf("the stopped command should be logged, got %q, %v", log, err)
	}
}
