package sessioncmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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
	alive, gone, err := agentLiveness(agent)
	if gone {
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
	return followTelemetry(st, row, *file, launch, agent, telemetryPoll, alive)
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
