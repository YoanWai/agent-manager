//go:build !windows

package termbrowser

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// shellClient runs body through the real exec path in place of
// terminal-browser. A freshly written script would do, but macOS scans a new
// executable on its first run for longer than the timeout under test.
func shellClient(body string, timeout time.Duration) *Client {
	client := New()
	client.Timeout = timeout
	client.run = func(ctx context.Context, _ string, _ ...string) ([]byte, []byte, int, error) {
		return runCommand(ctx, nil, "sh", "-c", body)
	}
	return client
}

func TestActionReportsARealExitCode(t *testing.T) {
	client := shellClient("echo out; echo err 1>&2; exit 3", 10*time.Second)
	result, err := client.Action(context.Background(), "k", "get url")
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Stdout: "out\n", Stderr: "err\n", ExitCode: 3}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
}

func TestActionTimeoutKillsTheProcessGroup(t *testing.T) {
	client := shellClient("echo partial; sleep 5", 200*time.Millisecond)
	started := time.Now()
	result, err := client.Action(context.Background(), "k", "get url")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || !strings.Contains(result.Stdout, "partial") {
		t.Fatalf("result = %+v, want a timeout carrying the partial output", result)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("the timed out run returned after %v; the sleeping child outlived the kill", elapsed)
	}
}

func TestActionSucceedsWhenABackgroundChildHoldsThePipes(t *testing.T) {
	client := shellClient("sleep 30 & echo $!", 10*time.Second)
	result, err := client.Action(context.Background(), "k", "get url")
	if pid, parseErr := strconv.Atoi(strings.TrimSpace(result.Stdout)); parseErr == nil {
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	}
	if err != nil {
		t.Fatalf("a child left holding the pipes failed the run: %v", err)
	}
	if result.ExitCode != 0 || result.TimedOut || strings.TrimSpace(result.Stdout) == "" {
		t.Fatalf("result = %+v, want a clean exit with the child's pid", result)
	}
}
