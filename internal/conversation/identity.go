package conversation

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// spawnDepth is how far below the launched process a CLI's agent can run: a
// launcher that spawns the CLI and waits, such as a Volta shim, and the CLI's
// own relaunch, as gemini does.
const spawnDepth = 2

// FromAgent asks whether process is the agent a launch started under agent,
// rather than a teammate, a CLI the agent ran as a tool, or one typed into
// the pane by hand, all of which inherit the session's environment. With
// spawned, a CLI's agent may also run up to spawnDepth levels below the
// launched process as long as it stays in that process's group, which a CLI
// the agent runs as a tool, in a group of its own, does not. A process that
// is gone is not the agent.
func FromAgent(agent, process int, spawned bool) (bool, error) {
	if process == agent || !spawned {
		return process == agent, nil
	}
	agentGroup, err := syscall.Getpgid(agent)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for range spawnDepth {
		group, err := syscall.Getpgid(process)
		if errors.Is(err, syscall.ESRCH) || (err == nil && group != agentGroup) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		parent, found, err := parentPID(process)
		if err != nil || !found || parent == agent {
			return found && parent == agent, err
		}
		process = parent
	}
	return false, nil
}

// parentPID reads a process's parent. ps exits non-zero for a pid that has
// gone, which is no parent rather than a failure.
func parentPID(pid int) (int, bool, error) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(out) == 0 {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("parent of %d: %w", pid, err)
	}
	parent, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return parent, err == nil, err
}
