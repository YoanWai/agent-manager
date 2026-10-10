package sessioncmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/conversation"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
)

const followTelemetryUsage = "usage: agent-manager follow-telemetry --file <telemetry file>"

const telemetryPoll = 2 * time.Second

// FollowTelemetry runs beside a gemini agent its launch started. It reads
// the telemetry file the launch pointed gemini at, records the conversation
// it names and empties it, so the file stays small and the row follows
// switches with no manager running. It ends after a last read once the agent
// has quit. A hangup or interrupt meant for the pane's agent leaves it
// running, since gemini can outlive its pane.
func FollowTelemetry(configDir string, args []string) error {
	set := flag.NewFlagSet("follow-telemetry", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	file := set.String("file", "", "the telemetry file the agent writes")
	if err := set.Parse(args); err != nil || set.NArg() > 0 || *file == "" {
		return errors.New(followTelemetryUsage)
	}
	row, launchEnv := os.Getenv(hooks.EnvSessionID), os.Getenv(hooks.EnvLaunch)
	if row == "" || launchEnv == "" {
		return nil
	}
	launch, err := strconv.ParseInt(launchEnv, 10, 64)
	if err != nil {
		return fmt.Errorf("%s=%q is not a launch", hooks.EnvLaunch, launchEnv)
	}
	agent, err := strconv.Atoi(os.Getenv(hooks.EnvAgentPID))
	if err != nil {
		return fmt.Errorf("%s is not a pid", hooks.EnvAgentPID)
	}
	group, err := syscall.Getpgid(agent)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT)
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	return followTelemetry(st, row, *file, launch, agent, telemetryPoll, func(int) bool { return groupAlive(group) })
}

func followTelemetry(st *store.Store, row, file string, launch int64, agent int, every time.Duration, alive func(int) bool) error {
	reader := &conversation.Reader{}
	for {
		running := alive(agent)
		err := reader.FollowTelemetry(file, agent, func(id string) (bool, error) {
			if !agentsession.ValidSessionID(id) {
				return true, nil
			}
			err := reportConversation(st, row, "gemini", id, launch, reportWait)
			return err == nil, err
		})
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(every)
	}
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
