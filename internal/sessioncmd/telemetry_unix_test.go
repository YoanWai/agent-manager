//go:build !windows

package sessioncmd

import (
	"os/exec"
	"syscall"
	"testing"
)

// Gemini's relaunched child logs on after the agent's own pid is killed
// alone, so the follower stays while anything is left in the group.
func TestGroupAliveWhileAnyProcessIsLeftInTheGroup(t *testing.T) {
	leader := exec.Command("sleep", "60")
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: leader.Process.Pid}
	if err := child.Start(); err != nil {
		leader.Process.Kill()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		leader.Process.Kill()
		child.Process.Kill()
		leader.Wait()
		child.Wait()
	})
	group := leader.Process.Pid
	if !groupAlive(group) {
		t.Fatal("a group with its leader and a child reads as gone")
	}
	leader.Process.Kill()
	leader.Wait()
	if !groupAlive(group) {
		t.Fatal("a group whose leader was killed alone reads as gone while its child runs")
	}
	child.Process.Kill()
	child.Wait()
	if groupAlive(group) {
		t.Fatal("an empty group reads as alive")
	}
}
