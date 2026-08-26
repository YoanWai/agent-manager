package sessioncmd

import (
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// RelaunchInPane starts a session's tool again inside the shell its pane
// already holds, for a session whose window is alive because only the agent
// exited. The command is typed into that shell rather than launched over a
// fresh window, so nothing about the pane is lost and the agent comes back
// as the shell's child, the shape every other session has. It carries the
// session environment inline as well, since a pane opened by an older
// manager holds a shell that was never given it.
func RelaunchInPane(driver *tmux.Driver, st *store.Store, hookManager *hooks.Manager, sess store.Session, tool config.Tool) (time.Time, error) {
	running, err := AgentRunning(driver, sess.ID)
	if err != nil {
		return time.Time{}, err
	}
	if running {
		return time.Time{}, fmt.Errorf("session %s is still running; revive brings back an agent that exited", sess.Name)
	}
	base := launch.ReviveCommand(tool, sess.AgentSessionID)
	command, env, err := launch.Environment(hookManager, sess.Tool, tool, base, sess.ID)
	if err != nil {
		return time.Time{}, err
	}
	if err := driver.SendKeys(sess.ID, tmux.ExportEnv(env, command), "Enter"); err != nil {
		return time.Time{}, err
	}
	launchedAt := time.Now()
	if err := st.SetAgentLaunchedAt(sess.ID, launchedAt); err != nil {
		return time.Time{}, err
	}
	if err := st.UpdateStatus(sess.ID, status.Starting); err != nil {
		return time.Time{}, err
	}
	// A leftover ack from the agent that exited must not swallow the first
	// finished alert of the one taking its place.
	if err := st.SetAcked(sess.ID, false); err != nil {
		return time.Time{}, err
	}
	return launchedAt, nil
}

// paneSettle is how long a busy pane is given to come back empty. A shell
// forks for its own startup work, and one sample cannot tell that apart
// from an agent.
const paneSettle = 250 * time.Millisecond

// AgentRunning reports whether a live pane still holds an agent: the pane's
// own pid is its shell, so anything under it is the agent. A sample that
// could not be read counts as running, because typing a command into a pane
// that turns out to hold an agent puts the text in its composer.
func AgentRunning(driver *tmux.Driver, sessID string) (bool, error) {
	busy, err := paneBusy(driver, sessID)
	if err != nil || !busy {
		return busy, err
	}
	time.Sleep(paneSettle)
	return paneBusy(driver, sessID)
}

func paneBusy(driver *tmux.Driver, sessID string) (bool, error) {
	pid, err := driver.PanePID(sessID)
	if err != nil {
		return true, err
	}
	stat, sampled := sysstat.Trees([]int{pid})[pid]
	if !sampled || !stat.OK {
		return true, fmt.Errorf("cannot read what session %s is running", sessID)
	}
	return stat.Procs > 1, nil
}
