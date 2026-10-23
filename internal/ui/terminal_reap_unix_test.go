//go:build unix

package ui

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A child of the manager that exits stays in the process table as a zombie
// until it is waited on. signal 0 still finds a zombie, and returns ESRCH
// only once the child has been reaped.
func TestStartReapedReapsTheExitedChild(t *testing.T) {
	cmd := exec.Command("true")
	if err := startReaped(cmd); err != nil {
		t.Fatalf("startReaped: %v", err)
	}
	pid := cmd.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still in the process table 5s after exiting (signal 0: %v): the child was never reaped", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartReapedReturnsTheStartError(t *testing.T) {
	if err := startReaped(exec.Command("/nonexistent/agent-manager-test-binary")); err == nil {
		t.Fatal("startReaped of a missing binary returned nil")
	}
}
