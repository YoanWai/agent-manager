package sessioncmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/conversation"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

const trackConversationUsage = "usage: agent-manager track-conversation --tool <tool> (--id <id> | --key <json-field>)"

// A report can arrive before the row it names exists, since a spawn creates
// the row once the pane is up. The wait stays short because the agent that
// fired the hook waits on it.
const (
	reportWait  = 2 * time.Second
	reportRetry = 100 * time.Millisecond
)

// TrackConversation records the conversation a running agent reports it is
// on, for the launch named in its environment. The agent's own hook runs it,
// with the id given directly or as a field of the hook's JSON input. Outside
// a managed session, in a pane an earlier manager launched without a launch
// stamp, and for any process but the agent its launch started, it does
// nothing.
func TrackConversation(configDir string, args []string, stdin io.Reader) error {
	set := flag.NewFlagSet("track-conversation", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	tool := set.String("tool", "", "the tool the reporting agent runs")
	id := set.String("id", "", "the conversation id")
	key := set.String("key", "", "the field of the JSON input that holds the conversation id")
	spawned := set.Bool("spawned", false, "the CLI may run its agent below the process its launch started, in its process group")
	if err := set.Parse(args); err != nil || set.NArg() > 0 || *tool == "" || (*id == "") == (*key == "") {
		return errors.New(trackConversationUsage)
	}
	var input []byte
	if *key != "" {
		// Read in full even when nothing is reported, so the hook runner
		// never meets a closed pipe.
		var err error
		if input, err = io.ReadAll(stdin); err != nil {
			return err
		}
	}
	row, launchEnv := os.Getenv(hooks.EnvSessionID), os.Getenv(hooks.EnvLaunch)
	if row == "" || launchEnv == "" {
		return nil
	}
	if launched, err := reportedByLaunchedAgent(os.Getppid(), *spawned); err != nil || !launched {
		return err
	}
	launch, err := strconv.ParseInt(launchEnv, 10, 64)
	if err != nil {
		return fmt.Errorf("%s=%q is not a launch", hooks.EnvLaunch, launchEnv)
	}
	reported := *id
	if *key != "" {
		if reported, err = jsonStringField(input, *key); err != nil {
			return err
		}
	}
	if !agentsession.ValidSessionID(reported) {
		return fmt.Errorf("refusing conversation id %q: not a plain token", reported)
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	return reportConversation(st, row, *tool, reported, launch, reportWait)
}

// reportedByLaunchedAgent asks whether reporter, the process that ran this
// report, is the agent its launch started. Every hook starts this binary as
// a direct child of the process that fired it, so the reporter is its
// parent.
func reportedByLaunchedAgent(reporter int, spawned bool) (bool, error) {
	launched, err := strconv.Atoi(os.Getenv(hooks.EnvAgentPID))
	if err != nil {
		return false, nil
	}
	return conversation.FromAgent(launched, reporter, spawned)
}

func jsonStringField(input []byte, key string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return "", fmt.Errorf("hook input: %w", err)
	}
	var value string
	if err := json.Unmarshal(fields[key], &value); err != nil {
		return "", fmt.Errorf("hook input has no string %q", key)
	}
	return value, nil
}

func reportConversation(st *store.Store, row, tool, reported string, launch int64, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		result, err := st.ReportAgentSessionID(row, tool, reported, launch)
		if err != nil || result != store.ReportEarly {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("session %s has no launch %d", row, launch)
		}
		time.Sleep(reportRetry)
	}
}

// FollowConversation moves a session onto the conversation its CLI's own
// state says the agent is on, for the CLIs that report no switch
// themselves and keep that state while the agent runs. Only state the agent
// this launch started left counts. reader keeps what it has read between
// calls.
func FollowConversation(st *store.Store, hookManager *hooks.Manager, reader *conversation.Reader, sess store.Session, style string) (store.Session, error) {
	if !conversation.Polled(style) {
		return sess, nil
	}
	agent, found, err := conversation.ReadAgent(hookManager.AgentFile(sess.ID))
	if err != nil || !found || agent.Launch != store.LaunchStamp(sess.AgentLaunchedAt) {
		return sess, err
	}
	report := func(id string) (bool, error) {
		if id == "" || !agentsession.ValidSessionID(id) {
			return true, nil
		}
		result, err := st.ReportAgentSessionID(sess.ID, sess.Tool, id, agent.Launch)
		if err != nil {
			return false, err
		}
		switch result {
		case store.ReportStale, store.ReportEarly:
			return false, nil
		case store.ReportAdopted, store.ReportUnchanged:
			sess.AgentSessionID = id
		}
		return true, nil
	}
	if style == "gemini" {
		err = reader.FollowTelemetry(hookManager.TelemetryFile(sess.ID, agent.Launch), agent.PID, report)
		return sess, err
	}
	id, err := reader.Current(style, agent, sess.LaunchTime())
	if err == nil {
		_, err = report(id)
	}
	return sess, err
}

// closingAgentWait is how long a revive gives an agent still closing, which
// for Hermes means writing the conversation it was on, within 200ms of a
// kill.
const closingAgentWait = time.Second

// deathSlack covers a closing agent's last write landing after its row was
// stamped dead.
const deathSlack = 2 * time.Second

// SettleConversation moves a session onto the conversation its CLI names
// only as it closes, once the agent has quit. Hermes rewrites its terminal's
// crumb on close, which is the only place a /branch or /resume shows.
// paneGone says the session's pane no longer exists. wait is how long to
// give an agent still closing; past it the row stays as it was.
func SettleConversation(st *store.Store, hookManager *hooks.Manager, sess store.Session, style string, paneGone bool, wait time.Duration) (store.Session, error) {
	if style != "hermes" {
		return sess, nil
	}
	agent, until, closed, err := closedAgent(hookManager.AgentFile(sess.ID), sess, paneGone, wait)
	if err != nil || !closed {
		return sess, err
	}
	id, err := conversation.HermesConversation(agent.TTY, sess.LaunchTime(), until, sess.Cwd)
	if err != nil || id == "" || id == sess.AgentSessionID || !agentsession.ValidSessionID(id) {
		return sess, err
	}
	if _, err := st.ReportAgentSessionID(sess.ID, sess.Tool, id, agent.Launch); err != nil {
		return sess, err
	}
	return st.Get(sess.ID)
}

// closedAgent waits up to wait for the agent this launch started to have
// closed, and says until when a write counts as its last. The shell that
// started the agent marks the record as it ends, so a CLI typed into the
// pane by hand afterwards writes too late to count, and an agent in a live
// pane has not closed until that mark lands. A pane that is gone took the
// shell with it: a dead row's terminal may since belong to another pane, so
// only a write from around its death counts, and a pane that died with no
// manager watching gives no time at all, where a zero until sets no bound.
func closedAgent(path string, sess store.Session, paneGone bool, wait time.Duration) (conversation.Agent, time.Time, bool, error) {
	for deadline := time.Now().Add(wait); ; time.Sleep(50 * time.Millisecond) {
		agent, found, err := conversation.ReadAgent(path)
		if err != nil || !found || agent.Launch != store.LaunchStamp(sess.AgentLaunchedAt) {
			return agent, time.Time{}, false, err
		}
		if agent.Ended {
			return agent, agent.Recorded, true, nil
		}
		if paneGone {
			running, err := agent.Running()
			if err != nil {
				return agent, time.Time{}, false, err
			}
			if !running && sess.Status == status.Dead {
				return agent, sess.LastStatusAt.Add(deathSlack), true, nil
			}
			if !running {
				return agent, time.Time{}, true, nil
			}
		}
		if time.Now().After(deadline) {
			return agent, time.Time{}, false, nil
		}
	}
}

// settleBeforeRevive brings a row onto the conversation its agent was last
// on before a revive builds its command, for a manager screen that was not
// running to follow it.
func settleBeforeRevive(st *store.Store, hookManager *hooks.Manager, sess store.Session, style string, paneGone bool) (store.Session, error) {
	sess, err := FollowConversation(st, hookManager, &conversation.Reader{}, sess, style)
	if err != nil {
		return sess, err
	}
	return SettleConversation(st, hookManager, sess, style, paneGone, closingAgentWait)
}
