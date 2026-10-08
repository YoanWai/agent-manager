//go:build unix

package remote

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperPID reads the pid a script echoed for the child it left holding
// the pipe, and ends that child when the test does.
func helperPID(t *testing.T, stdout []byte) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(string(stdout)))
	if err != nil {
		t.Fatalf("script printed %q", stdout)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// A ProxyCommand helper outlives a killed ssh; the deadline ends the whole
// process group rather than waiting on the helper's copy of the pipe.
func TestRunSSHDeadlineEndsTheProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	stdout, _, err := runSSH(ctx, []string{"sh", "-c", "sleep 10 & echo $!; sleep 10"})
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("call outlived its deadline by %v", elapsed-200*time.Millisecond)
	}
	if err == nil {
		t.Fatal("a call killed at its deadline succeeded")
	}
	pid := helperPID(t, stdout)
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper %d survived the deadline", pid)
		}
	}
}

// ssh has exited and its answer is whole, but a helper outside its control
// still holds the pipe.
func TestRunSSHStopsWaitingForAPipeAfterSSHExits(t *testing.T) {
	start := time.Now()
	stdout, _, err := runSSH(context.Background(), []string{"sh", "-c", "sleep 10 & echo $!"})
	if elapsed := time.Since(start); elapsed > waitDelay+time.Second {
		t.Fatalf("call waited %v on a pipe held after ssh exited", elapsed)
	}
	if err != nil {
		t.Fatalf("a whole answer failed: %v", err)
	}
	helperPID(t, stdout)
}
