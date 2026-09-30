package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// deadSessionHint names both ways back from a dead row: revive resumes the
// conversation it held, restart drops it.
const deadSessionHint = "session is dead - press v to revive or R to restart"

// shellPromptHint refuses to write into a shell. SendText pastes and then
// presses Enter, so a sentence meant for an agent would run as a command
// on the user's machine. Entering the session is how text reaches a shell,
// where what is typed is plainly a command.
func shellPromptHint(name string) string {
	return name + " is a shell, not an agent - enter it to type there"
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !m.services.tmux.Exists(sess.ID) {
		m.errBar.text = deadSessionHint
		return m, nil
	}
	m.errBar.text = ""
	if err := m.services.store.AcknowledgeFinished(sess.ID); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	return m, m.attachCmd(sess.ID)
}

// acknowledgeSelected marks the selected finished session idle and acked
// without entering it. Archived sessions keep their preserved status: the
// poller never re-derives it for them, so an ack would stick forever.
func (m *Model) acknowledgeSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok || sess.Archived || sess.Status != status.Finished {
		return m, nil
	}
	m.errBar.text = ""
	if err := m.services.store.AcknowledgeFinished(sess.ID); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.requestRefresh()
	return m, nil
}

func (m *Model) attachCmd(id string) tea.Cmd {
	// Flip the window back to auto-sizing so it fills the terminal on attach;
	// attachDoneMsg re-pins it to the preview width on detach. Clearing the
	// cached hash first keeps the poller from reading this reflow as
	// streaming output, same as the detach-side resize (reflowSessions).
	// A failure here still attaches: the worst outcome is a stale window
	// size, which beats locking the session out (issue #114).
	var prepErr error
	m.poller.reflowSessions([]string{id}, func() {
		prepErr = m.services.tmux.PrepareAttach(id)
	})
	if prepErr != nil {
		m.errBar.text = prepErr.Error()
	}
	return execTerminalProcess(m.services.tmux.AttachCommand(id), func(err error) tea.Msg {
		return attachDoneMsg{sessID: id, err: err}
	})
}

func (m *Model) reattach(id string, diffGen int) tea.Cmd {
	driver := m.services.tmux
	stor := m.services.store
	poller := m.poller
	return func() tea.Msg {
		if !driver.Exists(id) {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: errors.New(deadSessionHint)}
		}
		sess, err := stor.Get(id)
		if err != nil {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
		}
		if sess.Status == status.Finished {
			if err := stor.AcknowledgeFinished(sess.ID); err != nil {
				return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
			}
		}
		var prepErr error
		poller.reflowSessions([]string{id}, func() {
			prepErr = driver.PrepareAttach(id)
		})
		var warn string
		if prepErr != nil {
			warn = prepErr.Error()
		}
		return reattachPreparedMsg{sessID: id, diffGen: diffGen, warn: warn}
	}
}

// copyReplySelected puts the selected session's newest reply on the system
// clipboard without entering it.
func (m *Model) copyReplySelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || m.services.engine == nil || m.services.tmux == nil {
		return m, nil
	}
	sess := entry.sess
	m.errBar.text = ""
	// A shell has no reply, and its scrollback is the user's own commands
	// and their output rather than anything an agent said.
	if m.isShell(sess.Tool) {
		m.errBar.text = shellPromptHint(sess.Name)
		return m, nil
	}
	engine, driver := m.services.engine, m.services.tmux
	return m, func() tea.Msg {
		if !driver.Exists(sess.ID) {
			return errMsg{errors.New(deadSessionHint)}
		}
		pane, err := driver.CapturePaneHistory(sess.ID, quoteHistoryLines)
		if err != nil {
			return errMsg{err}
		}
		text, bounded, ok := engine.FullTurnText(sess.Tool, engine.Plain(sess.Tool, pane))
		if !ok {
			return replyCopiedMsg{name: sess.Name, tool: sess.Tool, unreadable: true}
		}
		if strings.TrimSpace(text) == "" {
			return replyCopiedMsg{name: sess.Name, tool: sess.Tool}
		}
		return copyTextCmd(text, func(chars int) tea.Msg {
			return replyCopiedMsg{chars: chars, name: sess.Name, tool: sess.Tool, unbounded: !bounded}
		})()
	}
}

// replyCopiedMsg reports a finished copy. No chars means the turn held
// nothing; unreadable means the tool draws no region a reply can be read
// from, which no amount of retrying will change; unbounded means nothing
// in the pane said where the turn began, so the copy is the whole screen
// rather than one answer.
type replyCopiedMsg struct {
	chars      int
	name       string
	tool       string
	unreadable bool
	unbounded  bool
}

// reviveSelected relaunches a dead session's tmux session under the same
// id, keeping its name, group, and history. Tools with a revive_command
// resume where they left off (e.g. claude --continue). On a group row it
// revives the whole subtree, mirroring the group kill.
func (m *Model) reviveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		sessions := m.sessionsInGroup(entry.group)
		if dead := deadSessions(sessions); len(dead) > 1 {
			m.confirm = confirmTarget{
				isGroup:  true,
				path:     entry.group,
				action:   actionRevive,
				batch:    true,
				sessions: dead,
				label: fmt.Sprintf("revive group %s (%d dead sessions)? brings them back.",
					displayGroup(entry.group), len(dead)),
			}
			m.mode = modeConfirmDelete
			return m, nil
		}
		return m.reviveMany(sessions, "no dead sessions to revive in "+entry.group)
	}
	set, err := m.sessionAndChildren(entry.sess)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	dead := false
	for _, sess := range set {
		if !m.services.tmux.Exists(sess.ID) {
			dead = true
			break
		}
	}
	if len(set) > 1 && dead {
		m.confirm = confirmTarget{
			action:   actionRevive,
			sessions: set,
			label: followConfirmLabel("revive", entry.sess.Name, len(set)-1,
				"brings it back.",
				"brings them back."),
		}
		m.mode = modeConfirmDelete
		return m, nil
	}
	// A pane the user quit the agent in is still a live window sitting at a
	// shell; the agent alone is gone, and it comes back inside that shell.
	if m.services.tmux.Exists(entry.sess.ID) {
		cmd, err := m.relaunchInPane(entry.sess)
		if err != nil {
			m.reportLaunchError(err, nil)
			return m, nil
		}
		m.errBar.text = m.degradedResumeNotice(entry.sess)
		return m, cmd
	}
	if err := m.reviveSession(entry.sess); err != nil {
		m.reportLaunchError(err, func() error { return m.reviveSession(entry.sess) })
		return m, nil
	}
	m.errBar.text = m.degradedResumeNotice(entry.sess)
	m.requestRefresh()
	return m, nil
}

// reviveAllDead relaunches every dead session in the current view, resuming
// each by its captured id where one exists.
func (m *Model) reviveAllDead() (tea.Model, tea.Cmd) {
	sessions := m.listedSessions()
	if dead := deadSessions(sessions); len(dead) > 1 {
		m.confirm = confirmTarget{
			action:   actionRevive,
			batch:    true,
			sessions: dead,
			label:    fmt.Sprintf("revive every dead session (%d)? brings them back.", len(dead)),
		}
		m.mode = modeConfirmDelete
		return m, nil
	}
	return m.reviveMany(sessions, "no dead sessions to revive")
}

// reviveMany relaunches every dead session in the list. It revives what it
// can and names the first failure rather than stopping, so one broken
// session does not block the rest.
func (m *Model) reviveMany(sessions []store.Session, emptyNotice string) (tea.Model, tea.Cmd) {
	revived, degraded := 0, 0
	var firstErr string
	for _, sess := range deadSessions(sessions) {
		if err := m.reviveSession(sess); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		revived++
		if m.degradedResumeNotice(sess) != "" {
			degraded++
		}
	}
	switch {
	case revived == 0 && firstErr == "":
		m.errBar.text = emptyNotice
	case firstErr != "":
		m.errBar.text = fmt.Sprintf("revived %d, first error: %s", revived, firstErr)
	case degraded > 0:
		m.errBar.text = fmt.Sprintf("revived %d, %d without a captured id (used --continue)", revived, degraded)
	default:
		m.errBar.text = ""
	}
	m.requestRefresh()
	return m, nil
}

func deadSessions(sessions []store.Session) []store.Session {
	var dead []store.Session
	for _, sess := range sessions {
		if sess.Status == status.Dead {
			dead = append(dead, sess)
		}
	}
	return dead
}

// sessionsInGroup lists the sessions the current view shows at or below a
// group, so a group action covers exactly the rows under it on screen.
func (m *Model) sessionsInGroup(path string) []store.Session {
	var sessions []store.Session
	for _, sess := range m.listedSessions() {
		if inGroupSubtree(sess.Group, path) {
			sessions = append(sessions, sess)
		}
	}
	return sessions
}

// degradedResumeNotice warns when a revived session had to fall back to the
// working directory's most recent conversation because its own conversation
// id was never captured, which resumes the wrong conversation whenever
// sessions share a directory.
func (m *Model) degradedResumeNotice(sess store.Session) string {
	tool, ok := m.services.cfg.Tools[sess.Tool]
	if !ok || sess.AgentSessionID != "" || tool.ResumeByIDCommand == "" || tool.ResumePickerCommand != "" {
		return ""
	}
	return fmt.Sprintf("revived %s with --continue: no conversation id captured, may resume the wrong conversation", sess.Name)
}

// reviveSession relaunches one dead session under its old id, keeping its
// name, group, and history. When the session's own conversation id was
// captured, it resumes that exact conversation via the tool's
// resume_by_id_command instead of the working directory's most recent one,
// which would be the wrong conversation whenever sessions share a cwd.
func (m *Model) reviveSession(sess store.Session) error {
	if m.services.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	paneWidth, paneHeight := m.paneTargetSize()
	result, err := m.services.lifecycle.Revive(sess, sessioncmd.PaneSize{Width: paneWidth, Height: paneHeight})
	if err != nil {
		return err
	}
	m.markFreshPane(sess.ID)
	m.bindReviveLocally(sess.ID, result.LaunchedAt)
	if m.focusPane.focus != nil {
		m.focusPane.focus.retryNow()
	}
	m.rebuildRows()
	return result.LabelError
}

// relaunchedMsg carries the result of starting an agent in a pane that was
// left on its shell.
type relaunchedMsg struct {
	sessID     string
	launchedAt time.Time
	err        error
}

// relaunchInPane builds the command that starts a session's tool again
// inside the shell its pane already holds, for a session whose window is
// alive because only the agent exited. The command is typed into that
// shell rather than launched over a fresh window, so nothing about the
// pane is lost and the agent comes back as the shell's child, the shape
// every other session has. It carries the session environment inline as
// well, since a pane opened by an older manager holds a shell that was
// never given it. The probe and the send run off the update path, where a
// pane that answers slowly would hold up the whole UI.
func (m *Model) relaunchInPane(sess store.Session) (tea.Cmd, error) {
	return func() tea.Msg {
		result, err := m.services.lifecycle.Revive(sess, sessioncmd.PaneSize{})
		if err != nil {
			return relaunchedMsg{sessID: sess.ID, err: err}
		}
		return relaunchedMsg{sessID: sess.ID, launchedAt: result.LaunchedAt}
	}, nil
}

// restartSelected asks to relaunch the selected session with an empty
// context: the same row, directory and tool, running a brand new
// conversation instead of resuming the one it held.
func (m *Model) restartSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		m.errBar.text = "restart applies to a session; pick one under " + displayGroup(entry.group)
		return m, nil
	}
	label := fmt.Sprintf("restart %s with an empty context? its current conversation is left behind.", entry.sess.Name)
	if m.services.tmux.Exists(entry.sess.ID) {
		label = fmt.Sprintf("restart %s with an empty context? ends the running agent and leaves its conversation behind.", entry.sess.Name)
	}
	m.confirm = confirmTarget{
		action:   actionRestart,
		sessions: []store.Session{entry.sess},
		label:    label,
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// restartSession relaunches a session's tool from scratch. The conversation
// it was resuming is retired rather than resumed, so the agent comes back
// with the same name, directory and group but no context to carry.
func (m *Model) restartSession(sess store.Session) error {
	if m.services.tmux.Exists(sess.ID) {
		m.unwatch(sess.ID)
	}
	paneWidth, paneHeight := m.paneTargetSize()
	var result sessioncmd.RelaunchResult
	var restartErr error
	m.poller.reflowSessions([]string{sess.ID}, func() {
		result, restartErr = m.services.lifecycle.Restart(sess, sessioncmd.PaneSize{Width: paneWidth, Height: paneHeight})
	})
	if restartErr != nil {
		return restartErr
	}
	m.markFreshPane(sess.ID)
	m.bindRestartLocally(sess.ID, result.Conversation, result.LaunchedAt)
	if m.focusPane.focus != nil {
		m.focusPane.focus.retryNow()
	}
	return result.LabelError
}

// restartLaunch builds what a restart runs: the tool's plain launch command,
// exactly as a brand new session gets it, plus a fresh conversation id for
// the tools that take one. Tools that mint their own id instead get nothing
// to carry, and the poller captures what they wrote.
func restartLaunch(tool config.Tool) (baseCommand, agentSessionID string) {
	if tool.SessionIDFlag == "" {
		return tool.Command, ""
	}
	agentSessionID = uuid.NewString()
	return tool.Command + " " + tool.SessionIDFlag + " " + agentSessionID, agentSessionID
}

// bindRestartLocally mirrors the store write in the loaded rows, so the list
// redraws on the new conversation before the next poll re-reads it.
func (m *Model) bindRestartLocally(id, agentSessionID string, launchedAt time.Time) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID != id {
			continue
		}
		if m.workspace.sessions[i].AgentSessionID != "" {
			m.workspace.sessions[i].RetiredAgentSessionID = m.workspace.sessions[i].AgentSessionID
		}
		m.workspace.sessions[i].AgentSessionID = agentSessionID
		m.workspace.sessions[i].AgentLaunchedAt = launchedAt
		m.workspace.sessions[i].Status = status.Starting
		m.workspace.sessions[i].Acked = false
	}
}

// bindReviveLocally mirrors the store write in the loaded rows, so the
// startup loader can paint before the next poll re-reads them.
func (m *Model) bindReviveLocally(id string, launchedAt time.Time) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID != id {
			continue
		}
		m.workspace.sessions[i].AgentLaunchedAt = launchedAt
		m.workspace.sessions[i].Status = status.Starting
		m.workspace.sessions[i].Acked = false
	}
	if sel, ok := m.selected(); ok && sel.ID == id {
		// A killed or archived row still holds its last pane; that would
		// count as painted and hide the loader.
		m.workspace.preview = ""
	}
}

// killSelected asks to end the selected session, or every live session
// under the selected group, freeing the RAM their agents hold while the
// rows stay put for v to revive.
func (m *Model) killSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		live, err := m.liveSessions(m.sessionsInGroup(entry.group))
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		if len(live) == 0 {
			m.errBar.text = "no live sessions to kill in " + entry.group
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionKill,
			sessions: live,
			label: fmt.Sprintf("kill group %s (%d live sessions)? frees their RAM, v revives them.",
				entry.group, len(live)),
		}
	} else {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		live := false
		for _, sess := range sessions {
			if m.services.tmux.Exists(sess.ID) {
				live = true
				break
			}
		}
		if !live {
			m.errBar.text = entry.sess.Name + " is already dead"
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionKill,
			sessions: sessions,
			label: followConfirmLabel("kill", entry.sess.Name, len(sessions)-1,
				"frees its RAM, v revives it.",
				"frees their RAM, v revives them."),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// killAllLive asks to end every live session in the current view, the
// batch counterpart to V.
func (m *Model) killAllLive() (tea.Model, tea.Cmd) {
	live, err := m.liveSessions(m.listedSessions())
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if len(live) == 0 {
		m.errBar.text = "no live sessions to kill"
		return m, nil
	}
	m.confirm = confirmTarget{
		action:   actionKill,
		sessions: live,
		label:    fmt.Sprintf("kill every live session (%d)? frees their RAM, v revives them.", len(live)),
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// liveSessions narrows a list to the sessions that still hold a tmux
// window. One pane listing answers for all of them, so a wide selection
// costs one tmux call rather than one per session.
func (m *Model) liveSessions(sessions []store.Session) ([]store.Session, error) {
	panes, err := m.services.tmux.Panes()
	if err != nil {
		return nil, err
	}
	var live []store.Session
	for _, sess := range sessions {
		if panes[sess.ID].PID > 0 {
			live = append(live, sess)
		}
	}
	return live, nil
}

// unwatch stops the focus watcher before a session is killed on purpose:
// the client going away then is the plan, not a loss to report.
func (m *Model) unwatch(id string) {
	if m.focusPane.focus != nil {
		m.focusPane.focus.unwatch(id)
	}
}

// killSession ends one session's tmux window, freeing everything its agent
// held, while the store row keeps the name, group, history and conversation
// id that revive needs. The pane is captured first so the preview still
// shows the agent's last output once the window is gone.
func (m *Model) killSession(sess store.Session) error {
	if !m.services.tmux.Exists(sess.ID) {
		return nil
	}
	m.unwatch(sess.ID)
	var killed store.Session
	var killErr error
	// Runs under the poller's lock so no pass can capture a half-killed
	// pane, and drops the pane hash the revived session would be compared
	// against.
	m.poller.reflowSessions([]string{sess.ID}, func() {
		killed, killErr = m.services.lifecycle.Kill(sess)
	})
	if killErr != nil {
		return killErr
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == sess.ID {
			m.workspace.sessions[i].Status = killed.Status
		}
	}
	return nil
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	if m.rail.showArchived {
		return m, nil
	}
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isRoot() {
		m.errBar.text = "root is the top level, not a group to archive"
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.services.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionArchive,
			sessions: subtree,
			label:    fmt.Sprintf("archive group %s (%d sessions)? frees their RAM, t to find them.", entry.group, len(subtree)),
		}
	} else {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionArchive,
			sessions: sessions,
			label: followConfirmLabel("archive", entry.sess.Name, len(sessions)-1,
				"frees its RAM, t to find it.",
				"frees their RAM, t to find them."),
		}
	}
	m.mode = modeConfirmDelete
	m.errBar.text = ""
	return m, nil
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	if !m.rail.showArchived {
		return m, nil
	}
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isRoot() {
		m.errBar.text = "root is the top level, not a group to restore"
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.services.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		archived := archivedSessions(subtree)
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionRestore,
			sessions: archived,
			label:    fmt.Sprintf("restore group %s (%d archived sessions)? brings them back.", entry.group, len(archived)),
		}
	} else {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		sessions = archivedSessions(sessions)
		m.confirm = confirmTarget{
			action:   actionRestore,
			sessions: sessions,
			label: followConfirmLabel("restore", entry.sess.Name, len(sessions)-1,
				"brings it back.",
				"brings them back."),
		}
	}
	m.mode = modeConfirmDelete
	m.errBar.text = ""
	return m, nil
}

func (m *Model) archiveConfirmed() error {
	ids := make([]string, 0, len(m.confirm.sessions))
	for _, sess := range m.confirm.sessions {
		ids = append(ids, sess.ID)
		if m.services.tmux.Exists(sess.ID) {
			m.unwatch(sess.ID)
		}
	}
	var result sessioncmd.ArchiveResult
	var archiveErr error
	archive := func() {
		result, archiveErr = m.services.lifecycle.ArchiveForHuman(sessioncmd.ArchiveSelection{
			Sessions:  m.confirm.sessions,
			GroupPath: groupPath(m.confirm),
		})
	}
	if len(ids) == 0 {
		archive()
	} else {
		m.poller.reflowSessions(ids, archive)
	}
	for i := range m.workspace.sessions {
		for _, archived := range result.Sessions {
			if m.workspace.sessions[i].ID == archived.ID {
				m.workspace.sessions[i].Status = archived.Status
			}
		}
	}
	if archiveErr != nil {
		return archiveErr
	}
	if !m.confirm.isGroup {
		for _, sess := range m.confirm.sessions {
			m.forgetLaunch(sess.ID)
		}
	}
	m.markArchivedLocally(result.Sessions, result.GroupPath)
	return nil
}

func (m *Model) restoreConfirmed() error {
	wasDead := make(map[string]bool, len(m.confirm.sessions))
	for _, sess := range m.confirm.sessions {
		wasDead[sess.ID] = !m.services.tmux.Exists(sess.ID)
	}
	paneWidth, paneHeight := m.paneTargetSize()
	result, err := m.services.lifecycle.RestoreForHuman(sessioncmd.ArchiveSelection{
		Sessions:  m.confirm.sessions,
		GroupPath: groupPath(m.confirm),
	}, sessioncmd.PaneSize{Width: paneWidth, Height: paneHeight})
	revived := false
	for _, sess := range result.Sessions {
		if !wasDead[sess.ID] {
			continue
		}
		revived = true
		m.markFreshPane(sess.ID)
		m.bindReviveLocally(sess.ID, sess.AgentLaunchedAt)
	}
	if revived && m.focusPane.focus != nil {
		m.focusPane.focus.retryNow()
	}
	if err != nil {
		return err
	}
	m.markRestoredLocally(result.Sessions, result.GroupPath)
	m.errBar.text = ""
	if result.LabelError != nil {
		m.errBar.text = result.LabelError.Error()
	}
	return nil
}

func (m *Model) deleteConfirmed() (sessioncmd.DeleteResult, error) {
	ids := make([]string, 0, len(m.confirm.sessions))
	for _, sess := range m.confirm.sessions {
		ids = append(ids, sess.ID)
		m.unwatch(sess.ID)
	}
	var result sessioncmd.DeleteResult
	var deleteErr error
	remove := func() {
		result, deleteErr = m.services.lifecycle.DeleteForHuman(sessioncmd.DeleteSelection{
			Sessions:     m.confirm.sessions,
			GroupPath:    groupPath(m.confirm),
			ArchivedOnly: m.confirm.archivedOnly,
		})
	}
	if len(ids) == 0 {
		remove()
	} else {
		m.poller.reflowSessions(ids, remove)
	}
	for _, sess := range result.Deleted {
		delete(m.ledger.pickedRepos, sess.ID)
		delete(m.ledger.awaitedRenames, sess.ID)
		m.forgetLaunch(sess.ID)
		m.removeSessionLocally(sess.ID)
	}
	if len(result.RemovedGroups) > 0 {
		for _, path := range result.RemovedGroups {
			delete(m.rail.collapsed, path)
		}
		m.persistCollapsed()
		m.pruneGroupsLocally(result.RemovedGroups)
	}
	if result.Notice != "" {
		m.errBar.text = result.Notice
	}
	return result, deleteErr
}

// removeSessionLocally takes a deleted row off the loaded list right away,
// so it leaves the screen on this frame instead of waiting for the next
// poll to confirm what the store already knows.
func (m *Model) removeSessionLocally(id string) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == id {
			m.workspace.sessions = append(m.workspace.sessions[:i], m.workspace.sessions[i+1:]...)
			break
		}
	}
	m.markSession(id, goneMark{deleted: true})
	m.rebuildRows()
}

// markArchivedLocally flags the confirmed archive in the loaded rows, so
// they leave the active view on this frame instead of first showing the
// dead state their kill just gave them. A group archive also flags the
// group itself and every group under it, which is what hides the whole
// subtree from the active view.
func (m *Model) markArchivedLocally(sessions []store.Session, groupPath string) {
	changed := false
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].Archived {
			continue
		}
		for _, sess := range sessions {
			if m.workspace.sessions[i].ID != sess.ID {
				continue
			}
			m.workspace.sessions[i].Archived = true
			m.markSession(sess.ID, goneMark{archived: true})
			changed = true
			break
		}
	}
	if groupPath != "" {
		for _, path := range append([]string{groupPath}, m.subgroupPaths(groupPath)...) {
			if m.workspace.archivedGroups == nil {
				m.workspace.archivedGroups = map[string]bool{}
			}
			m.workspace.archivedGroups[path] = true
			m.markGroup(path, goneMark{archived: true})
		}
		changed = true
	}
	if changed {
		m.rebuildRows()
	}
}

func (m *Model) subgroupPaths(path string) []string {
	var out []string
	prefix := path + "/"
	for _, group := range m.workspace.groups {
		if strings.HasPrefix(group, prefix) {
			out = append(out, group)
		}
	}
	return out
}

// markRestoredLocally mirrors a completed restore in the loaded rows and
// group flags, so what came back changes views on this frame rather than
// waiting for the next poll. A group restore unarchives the subtree; a
// single restore also clears its group's ancestors, which is what the
// store write just did to keep a restored session under a live home.
func (m *Model) markRestoredLocally(restored []store.Session, groupPath string) {
	byID := make(map[string]bool, len(restored))
	for _, sess := range restored {
		byID[sess.ID] = true
	}
	for i := range m.workspace.sessions {
		if !m.workspace.sessions[i].Archived {
			continue
		}
		if byID[m.workspace.sessions[i].ID] || (groupPath != "" && inGroupSubtree(m.workspace.sessions[i].Group, groupPath)) {
			m.workspace.sessions[i].Archived = false
			m.markSession(m.workspace.sessions[i].ID, goneMark{archived: false})
		}
	}
	if groupPath != "" {
		for _, path := range append([]string{groupPath}, m.subgroupPaths(groupPath)...) {
			delete(m.workspace.archivedGroups, path)
			m.markGroup(path, goneMark{archived: false})
		}
	} else {
		for _, sess := range restored {
			for path := sess.Group; path != ""; path = parentGroup(path) {
				delete(m.workspace.archivedGroups, path)
				m.markGroup(path, goneMark{archived: false})
			}
		}
	}
	m.rebuildRows()
}

// markGroup records the archive state this run just gave a group path,
// so stale polls predating the change are reconciled on arrival instead
// of undoing it for a frame.
func (m *Model) markGroup(path string, mark goneMark) {
	if m.ledger.goneGroups == nil {
		m.ledger.goneGroups = map[string]goneMark{}
	}
	mark.at = time.Now()
	m.ledger.goneGroups[path] = mark
}

// pruneGroupsLocally drops the removed group paths from the loaded tree,
// so a deleted group's header goes with its sessions instead of hanging
// around empty until the next poll. Each path is recorded for the stale
// listing filter, which is what keeps an in-flight poll from restoring it.
func (m *Model) pruneGroupsLocally(removed []string) {
	gone := make(map[string]bool, len(removed))
	for _, path := range removed {
		gone[path] = true
		m.markGroup(path, goneMark{deleted: true})
	}
	groups := make([]string, 0, len(m.workspace.groups))
	for _, group := range m.workspace.groups {
		if !gone[group] {
			groups = append(groups, group)
		}
	}
	m.workspace.groups = groups
	for _, path := range removed {
		delete(m.workspace.groupPaths, path)
		delete(m.workspace.groupWorktrees, path)
		delete(m.workspace.archivedGroups, path)
	}
	m.rebuildRows()
}

func (m *Model) sessionAndChildren(sess store.Session) ([]store.Session, error) {
	kids, err := m.services.store.Children(sess.ID)
	if err != nil {
		return nil, err
	}
	out := make([]store.Session, 0, 1+len(kids))
	out = append(out, sess)
	return append(out, kids...), nil
}

func followConfirmLabel(verb, name string, extra int, one, many string) string {
	if extra <= 0 {
		return fmt.Sprintf("%s %s? %s", verb, name, one)
	}
	unit := "terminal"
	if extra != 1 {
		unit = "terminals"
	}
	return fmt.Sprintf("%s %s and %d %s? %s", verb, name, extra, unit, many)
}

func groupPath(confirm confirmTarget) string {
	if confirm.isGroup {
		return confirm.path
	}
	return ""
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.errBar.text = "root is the top level; delete the sessions under it instead"
		return
	}
	if !entry.isGroup {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return
		}
		m.confirm = confirmTarget{
			label: followConfirmLabel("delete", entry.sess.Name, len(sessions)-1,
				"kills its tmux session.",
				"kills their tmux sessions."),
			sessions: sessions,
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.services.store.SessionsInSubtree(entry.group)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if m.rail.showArchived {
		m.confirm = archivedGroupDelete(entry.group, subtree)
	} else {
		m.confirm = m.wholeGroupDelete(entry.group, subtree)
	}
	m.mode = modeConfirmDelete
}

// wholeGroupDelete targets the group as the active view shows it: the
// group ceases to exist, so its subtree goes with it, archived sessions
// included, leaving nothing stranded under a group that is gone.
func (m *Model) wholeGroupDelete(path string, subtree []store.Session) confirmTarget {
	subgroups := 0
	for _, g := range m.workspace.groups {
		if strings.HasPrefix(g, path+"/") {
			subgroups++
		}
	}
	return confirmTarget{
		isGroup:  true,
		path:     path,
		sessions: subtree,
		label: fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions.",
			path, subgroups, len(subtree)),
	}
}

// archivedGroupDelete targets only what the archived view shows: the
// archived sessions under the group. The live sessions and the group
// itself belong to the active view and survive; the group row goes only
// once nothing is left beneath it.
func archivedGroupDelete(path string, subtree []store.Session) confirmTarget {
	archived := archivedSessions(subtree)
	return confirmTarget{
		isGroup:      true,
		archivedOnly: true,
		path:         path,
		sessions:     archived,
		label: fmt.Sprintf("delete %s from the archive (%d archived sessions)? kills their tmux sessions, live ones stay.",
			path, len(archived)),
	}
}

func archivedSessions(sessions []store.Session) []store.Session {
	var archived []store.Session
	for _, sess := range sessions {
		if sess.Archived {
			archived = append(archived, sess)
		}
	}
	return archived
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The card advertises y/enter and n/esc; anything else leaves it up rather
	// than dismissing the question the user has not answered.
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "enter", "n", "esc":
	default:
		return m, nil
	}
	// A relaunch the manager refused opened the hint dialog; every other
	// answer falls back to the list.
	defer func() {
		if m.mode != modeLaunchHint {
			m.mode = modeList
		}
	}()
	switch msg.String() {
	case "y", "enter":
		switch m.confirm.action {
		case actionArchive:
			if err := m.archiveConfirmed(); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
			m.errBar.text = ""
		case actionRestore:
			if err := m.restoreConfirmed(); err != nil {
				m.reportLaunchError(err, m.restoreConfirmed)
				return m, nil
			}
		case actionKill:
			for _, sess := range m.confirm.sessions {
				if err := m.killSession(sess); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
			}
			m.errBar.text = ""
			m.rebuildRows()
		case actionRestart:
			for _, sess := range m.confirm.sessions {
				if err := m.restartSession(sess); err != nil {
					m.reportLaunchError(err, func() error { return m.restartSession(sess) })
					return m, nil
				}
			}
			m.errBar.text = ""
			m.rebuildRows()
		case actionRevive:
			if m.confirm.batch {
				panes, err := m.services.tmux.Panes()
				if err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				var stillDead []store.Session
				for _, sess := range m.confirm.sessions {
					if panes[sess.ID].PID == 0 {
						stillDead = append(stillDead, sess)
					}
				}
				m.confirm = confirmTarget{}
				return m.reviveMany(stillDead, "")
			}
			for _, sess := range m.confirm.sessions {
				if m.services.tmux.Exists(sess.ID) {
					continue
				}
				if err := m.reviveSession(sess); err != nil {
					m.reportLaunchError(err, func() error { return m.reviveSession(sess) })
					return m, nil
				}
			}
			m.errBar.text = ""
		case actionDelete:
			if _, err := m.deleteConfirmed(); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
		default:
			m.errBar.text = fmt.Sprintf("unknown confirm action %q", m.confirm.action)
			return m, nil
		}
		m.confirm = confirmTarget{}
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}
