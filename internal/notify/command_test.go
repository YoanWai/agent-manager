package notify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestCommandEnvContract(t *testing.T) {
	event := Event{ID: "sess-1", Session: "deploy", Tool: "claude", Dir: "/src/api-worktrees/am-x", Branch: "am/fix-login"}
	for kind, want := range map[Kind][2]string{
		Waiting:  {"waiting", "◆ Waiting for your input"},
		Finished: {"finished", "● Finished"},
		Errored:  {"errored", "✕ Errored"},
	} {
		event.Kind = kind
		got := CommandEnv(event)
		expected := []string{
			"AM_NOTIFY_KIND=" + want[0],
			"AM_SESSION_ID=sess-1",
			"AM_SESSION_NAME=deploy",
			"AM_TOOL=claude",
			"AM_CWD=/src/api-worktrees/am-x",
			"AM_BRANCH=am/fix-login",
			"AM_TITLE=deploy · claude",
			"AM_BODY=" + want[1],
		}
		if !slices.Equal(got, expected) {
			t.Fatalf("%s env = %q, want %q", want[0], got, expected)
		}
	}
	if env := CommandEnv(Event{ID: "x"}); env != nil {
		t.Fatalf("an event of no kind should have no env, got %q", env)
	}
}

func shellOutFile(t *testing.T) (dir, out string) {
	t.Helper()
	dir = t.TempDir()
	return dir, filepath.Join(dir, "out")
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

func TestRunCommandSkipsABlankCommand(t *testing.T) {
	defer func(previous func([]string, string) (string, error)) { runShell = previous }(runShell)
	ran := false
	runShell = func([]string, string) (string, error) {
		ran = true
		return "", nil
	}
	RunCommand("  ", t.TempDir(), Event{ID: "sess-1", Kind: Waiting})
	if ran {
		t.Fatal("a blank command should not reach the shell")
	}
}

func TestCommandLogStartsOverPastItsLimit(t *testing.T) {
	dir, _ := shellOutFile(t)
	path := filepath.Join(dir, CommandLog)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", commandLogLimit+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	RunCommand(`exit 1`, dir, Event{ID: "sess-1", Session: "s", Kind: Waiting})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > commandLogLimit {
		t.Fatalf("log is %d bytes, want it started over below %d", info.Size(), commandLogLimit)
	}
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

func TestCappedBufferTakesEveryWrite(t *testing.T) {
	buffer := &cappedBuffer{limit: 4}
	for _, chunk := range []string{"ab", "cdef", "gh"} {
		if n, err := buffer.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want every byte taken", chunk, n, err)
		}
	}
	if got := buffer.String(); got != "abcd" {
		t.Fatalf("kept %q, want abcd", got)
	}
}

func TestLogCommandFailureNamesTheEventAndCause(t *testing.T) {
	dir := t.TempDir()
	LogCommandFailure(dir, Event{ID: "sess-1", Kind: Waiting}, errors.New("reading the notify command: database is closed"))
	log, err := os.ReadFile(filepath.Join(dir, CommandLog))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"waiting", "sess-1", "database is closed"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("log %q does not name %q", log, want)
		}
	}
}

// Deliveries log from goroutines of their own; every line lands whole.
func TestLogCommandFailureFromConcurrentDeliveries(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			LogCommandFailure(dir, Event{ID: fmt.Sprintf("sess-%d", i), Kind: Errored}, errors.New("exit status 1"))
		}()
	}
	wg.Wait()
	log, err := os.ReadFile(filepath.Join(dir, CommandLog))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(log), "exit status 1\n"); lines != 20 {
		t.Fatalf("log holds %d whole lines, want 20:\n%s", lines, log)
	}
}

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
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the command's timeout", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
