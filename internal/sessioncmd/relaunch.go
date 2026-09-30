package sessioncmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
)

func SnapshotRelaunch(st *store.Store, sess store.Session, tool config.Tool, agentSessionID string) error {
	if agentSessionID != "" || tool.SessionStore == "" {
		return nil
	}
	snapshot, ok := agentsession.Snapshot(tool.SessionStore, sess.Cwd)
	if !ok {
		snapshot = nil
	}
	return st.SetRelaunchSnapshot(sess.ID, snapshot)
}

const pickerInjectionTimeout = 45 * time.Second

// InjectPickerKeys opens a picker that exists only inside a running TUI. The
// keys wait for the caret to rest on the tool's composer row, since a tool
// drawing inline (agy) leaves its last frame, composer and all, in the pane
// it exited.
func InjectPickerKeys(driver *tmux.Driver, sessID string, tool config.Tool) {
	composer := tool.InputPrefix
	if composer == "" {
		composer = tool.ActivityCutoff
	}
	keys := tool.ResumePickerKeys
	if composer == "" || keys == "" {
		return
	}
	re, err := regexp.Compile(composer)
	if err != nil {
		return
	}
	go func() {
		deadline := time.Now().Add(pickerInjectionTimeout)
		for time.Now().Before(deadline) {
			if !driver.Exists(sessID) {
				return
			}
			row, ok := caretRow(driver, sessID)
			if loc := re.FindStringIndex(row); !ok || loc == nil || loc[0] != 0 {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			if err := driver.SendKeys(sessID, keys, "Enter"); err != nil {
				return
			}
			if keysStayInComposer(driver, sessID, keys) {
				_ = driver.SendKeys(sessID, "Enter")
			}
			return
		}
	}()
}

// completionSettle is how long the keys must stay on the caret row before
// the first Enter counts as having accepted a completion rather than run it.
const completionSettle = 1500 * time.Millisecond

// keysStayInComposer tells a composer that took the first Enter as
// accepting its completion, and still holds the keys, from a picker that
// opened and took the caret off them, where one more Enter would pick its
// top row for the user. A picker slow to draw leaves the keys up for a
// moment, so they have to stay for the whole settle.
func keysStayInComposer(driver *tmux.Driver, sessID, keys string) bool {
	deadline := time.Now().Add(completionSettle)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if row, ok := caretRow(driver, sessID); !ok || !strings.Contains(row, keys) {
			return false
		}
	}
	return true
}

func caretRow(driver *tmux.Driver, sessID string) (string, bool) {
	_, y, err := driver.Cursor(sessID)
	if err != nil {
		return "", false
	}
	pane, err := driver.CapturePane(sessID)
	if err != nil {
		return "", false
	}
	rows := strings.Split(ansi.Strip(pane), "\n")
	if y < 0 || y >= len(rows) {
		return "", false
	}
	return rows[y], true
}

// RelaunchInPane starts a session's tool again inside the shell its pane
// already holds, so the pane keeps everything its last life left there. The
// session environment rides along inline because a pane opened by an older
// manager holds a shell that was never given it.
func RelaunchInPane(driver *tmux.Driver, st *store.Store, hookManager *hooks.Manager, sess store.Session, tool config.Tool) (time.Time, error) {
	running, err := AgentRunning(driver, sess.ID)
	if err != nil {
		return time.Time{}, err
	}
	if running {
		return time.Time{}, fmt.Errorf("session %s is still running; revive brings back an agent that exited", sess.Name)
	}
	if err := SnapshotRelaunch(st, sess, tool, sess.AgentSessionID); err != nil {
		return time.Time{}, err
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
