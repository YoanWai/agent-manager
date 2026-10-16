//go:build !windows

package sessioncmd

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// agentLiveness tells the follower when the agent is gone, or that it
// already is. Gemini logs from a child it relaunches itself in, so the
// agent's process group, not its pid, is what has to empty.
func agentLiveness(agent int) (alive func(int) bool, gone bool, err error) {
	group, err := syscall.Getpgid(agent)
	if errors.Is(err, syscall.ESRCH) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func(int) bool { return groupAlive(group) }, false, nil
}

// groupAlive reports whether any process but this follower is left in the
// agent's process group. Gemini logs from a child it relaunches itself in,
// which outlives the agent's own pid when that is killed alone.
func groupAlive(group int) bool {
	ps := exec.Command("ps", "-axo", "pid=,pgid=,stat=")
	// ps would otherwise count itself, started in this follower's group.
	ps.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := ps.Output()
	if err != nil {
		// Following on is harmless; stopping would leave the file to grow.
		return true
	}
	self := os.Getpid()
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasPrefix(fields[2], "Z") {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		pgid, pgidErr := strconv.Atoi(fields[1])
		if pidErr == nil && pgidErr == nil && pgid == group && pid != self {
			return true
		}
	}
	return false
}
