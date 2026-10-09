// Package conversation tells which conversation a running agent is on from
// state its CLI keeps for itself, read and never written, for the CLIs that
// report no switch through an extension point. Every answer is keyed by the
// pid a launch started its agent under, which the launch records in a file of
// the manager's own.
package conversation

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/shirou/gopsutil/v4/process"
)

// Agent is what a launch recorded about the agent it started: its pid, the
// launch stamp it was started under, and the terminal it runs in. Ended
// says the agent has quit and the shell that started it lives on, and
// Recorded is when the record was last written: as the agent started, or
// as it ended.
type Agent struct {
	PID      int
	Launch   int64
	TTY      string
	Ended    bool
	Recorded time.Time
}

const endedLine = "ended"

// RecordCommand is the shell line a launch runs before it execs its agent,
// which writes the agent's own pid, the launch and the pane's terminal to
// path.
func RecordCommand(path string, launch int64) string {
	return `printf '%s %s %s\n' "$$" ` + strconv.FormatInt(launch, 10) + ` "$(tty)" > ` + tmux.ShellQuote(path)
}

// EndedCommand is the shell line a launch runs once its agent has quit,
// in the shell that started it, which marks the record at path ended.
func EndedCommand(path string) string {
	return "printf '" + endedLine + "\\n' >> " + tmux.ShellQuote(path)
}

// ReadAgent reads the record a launch left at path. A session no launch of
// this release started has none.
func ReadAgent(path string) (Agent, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Agent{}, false, nil
	}
	if err != nil {
		return Agent{}, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Agent{}, false, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return Agent{}, false, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// The launch shell truncates the file before it writes the line.
	if lines[0] == "" {
		return Agent{}, false, nil
	}
	fields := strings.SplitN(lines[0], " ", 3)
	if len(fields) != 3 || len(lines) > 2 || (len(lines) == 2 && lines[1] != endedLine) {
		return Agent{}, false, fmt.Errorf("%s: not an agent record", path)
	}
	pid, pidErr := strconv.ParseInt(fields[0], 10, 32)
	launch, launchErr := strconv.ParseInt(fields[1], 10, 64)
	if pidErr != nil || launchErr != nil || pid <= 0 {
		return Agent{}, false, fmt.Errorf("%s: not an agent record", path)
	}
	return Agent{PID: int(pid), Launch: launch, TTY: fields[2], Ended: len(lines) == 2, Recorded: info.ModTime()}, true, nil
}

// startSlack covers how coarsely a process's start time is known: Linux
// counts it from a boot time kept in whole seconds.
const startSlack = time.Second

// Running reports whether the agent still runs. Once it has quit, the
// system can hand its pid to another process, which started after the
// agent wrote its record.
func (agent Agent) Running() (bool, error) {
	if agent.Ended || !Alive(agent.PID) {
		return false, nil
	}
	started, err := (&process.Process{Pid: int32(agent.PID)}).CreateTime()
	if err != nil {
		if !Alive(agent.PID) {
			return false, nil
		}
		return false, fmt.Errorf("start time of process %d: %w", agent.PID, err)
	}
	return !time.UnixMilli(started).After(agent.Recorded.Add(startSlack)), nil
}

// Alive reports whether a process runs under pid.
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
