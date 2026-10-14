//go:build !windows

package notify

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The timeout stops what the command started, not only the shell.
func TestRunCommandStopsTheCommandsChildrenAtTheTimeout(t *testing.T) {
	defer func(previous time.Duration) { commandTimeout = previous }(commandTimeout)
	commandTimeout = 300 * time.Millisecond
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	RunCommand(`sleep 30 & echo $! > `+pidFile+`; wait`, dir, Event{ID: "sess-1", Session: "s", Kind: Waiting})
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the child never started: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for running(t, pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the command's timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// running reports whether pid is a live process. A killed child the shell
// never reaped can linger as a zombie on a host whose init does not reap
// orphans, and a zombie still answers kill -0. A state it cannot read
// fails the test rather than passing for a stopped child.
func running(t *testing.T, pid int) bool {
	t.Helper()
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	} else if err != nil {
		t.Fatalf("signal 0 to %d: %v", pid, err)
	}
	state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	var exit *exec.ExitError
	// ps exits non-zero with nothing printed when the pid went away
	// between the signal and the lookup.
	if errors.As(err, &exit) && strings.TrimSpace(string(state)) == "" {
		return false
	}
	if err != nil {
		t.Fatalf("reading the state of %d: %v", pid, err)
	}
	return !strings.HasPrefix(strings.TrimSpace(string(state)), "Z")
}

// A command that prints far more than the log keeps is drained, not
// buffered whole, and only its start is kept.
func TestRunShellKeepsOnlyTheStartOfTheOutput(t *testing.T) {
	output, err := runBoundedShell(nil, `i=0; while [ $i -lt 2000 ]; do echo 0123456789abcdef0123456789abcdef; i=$((i+1)); done; echo tail >&2`)
	if err != nil {
		t.Fatalf("the command should finish: %v", err)
	}
	if len(output) != commandOutputLimit || !strings.HasPrefix(output, "0123456789abcdef") {
		t.Fatalf("kept %d bytes starting %q, want the first %d", len(output), output[:min(len(output), 16)], commandOutputLimit)
	}
}

// The command reads the event from its environment, and agent-written text
// in it is data: nothing the session is called is ever run by the shell.
func TestRunCommandPassesTheEventThroughTheEnvironment(t *testing.T) {
	dir, out := shellOutFile(t)
	pwned := filepath.Join(dir, "pwned")
	event := Event{ID: "sess-1", Session: "$(touch " + pwned + ")", Tool: "codex", Kind: Waiting, Dir: dir}
	RunCommand(`printf '%s|%s|%s|%s' "$AM_NOTIFY_KIND" "$AM_SESSION_NAME" "$AM_CWD" "$AM_BODY" > `+out, dir, event)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the command did not run: %v", err)
	}
	if want := "waiting|$(touch " + pwned + ")|" + dir + "|◆ Waiting for your input"; string(got) != want {
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
	RunCommand(`echo "no route to ntfy" >&2; exit 3`, dir, Event{ID: "sess-1", Session: "s", Kind: Errored})
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

func TestRunCommandStopsAWedgedCommand(t *testing.T) {
	defer func(previous time.Duration) { commandTimeout = previous }(commandTimeout)
	commandTimeout = 200 * time.Millisecond
	dir, _ := shellOutFile(t)
	start := time.Now()
	RunCommand(`sleep 30`, dir, Event{ID: "sess-1", Session: "s", Kind: Waiting})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a wedged command held delivery for %v", elapsed)
	}
	if log, err := os.ReadFile(filepath.Join(dir, CommandLog)); err != nil || !strings.Contains(string(log), "killed") {
		t.Fatalf("the timeout should be logged, got %q, %v", log, err)
	}
}

func TestCommandLogIsPrivate(t *testing.T) {
	dir := t.TempDir()
	LogCommandFailure(dir, Event{ID: "sess-1", Kind: Waiting}, errors.New("curl: (22) 401 for https://ntfy.example/topic?auth=secret"))
	info, err := os.Stat(filepath.Join(dir, CommandLog))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("log mode = %o, want 600", got)
	}
}
