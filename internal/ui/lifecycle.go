package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.err = "session is dead - press v to revive"
		return m, nil
	}
	m.err = ""
	if err := m.acknowledgeFinished(sess); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, m.attachCmd(sess.ID)
}

// acknowledgeFinished marks a finished session idle and acked so entering it
// clears the alert while the pane still shows the acknowledged turn.
func (m *Model) acknowledgeFinished(sess store.Session) error {
	if sess.Status != status.Finished {
		return nil
	}
	if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		return err
	}
	return m.store.SetAcked(sess.ID, true)
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
		prepErr = m.tmux.PrepareAttach(id)
	})
	if prepErr != nil {
		m.err = prepErr.Error()
	}
	return tea.ExecProcess(m.tmux.AttachCommand(id), func(err error) tea.Msg {
		return attachDoneMsg{sessID: id, err: err}
	})
}

func (m *Model) reattach(id string, diffGen int) tea.Cmd {
	driver := m.tmux
	stor := m.store
	poller := m.poller
	return func() tea.Msg {
		if !driver.Exists(id) {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: errors.New("session is dead - press v to revive")}
		}
		sess, err := stor.Get(id)
		if err != nil {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
		}
		if sess.Status == status.Finished {
			if err := stor.UpdateStatus(sess.ID, status.Idle); err != nil {
				return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
			}
			if err := stor.SetAcked(sess.ID, true); err != nil {
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
		return m.reviveMany(m.sessionsInGroup(entry.group), "no dead sessions to revive in "+entry.group)
	}
	if err := m.reviveSession(entry.sess); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = m.degradedResumeNotice(entry.sess)
	m.requestRefresh()
	return m, nil
}

// reviveAllDead relaunches every dead session in the current view, resuming
// each by its captured id where one exists.
func (m *Model) reviveAllDead() (tea.Model, tea.Cmd) {
	return m.reviveMany(m.visibleSessions(), "no dead sessions to revive")
}

// reviveMany relaunches every dead session in the list. It revives what it
// can and names the first failure rather than stopping, so one broken
// session does not block the rest.
func (m *Model) reviveMany(sessions []store.Session, emptyNotice string) (tea.Model, tea.Cmd) {
	revived, degraded := 0, 0
	var firstErr string
	for _, sess := range sessions {
		if sess.Status != status.Dead {
			continue
		}
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
		m.err = emptyNotice
	case firstErr != "":
		m.err = fmt.Sprintf("revived %d, first error: %s", revived, firstErr)
	case degraded > 0:
		m.err = fmt.Sprintf("revived %d, %d without a captured id (used --continue)", revived, degraded)
	default:
		m.err = ""
	}
	m.requestRefresh()
	return m, nil
}

// sessionsInGroup lists the sessions the current view shows at or below a
// group, so a group action covers exactly the rows under it on screen.
func (m *Model) sessionsInGroup(path string) []store.Session {
	var sessions []store.Session
	for _, sess := range m.visibleSessions() {
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
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok || sess.AgentSessionID != "" || tool.ResumeByIDCommand == "" {
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
	if m.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		return fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if info, err := os.Stat(sess.Cwd); err != nil || !info.IsDir() {
		return fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	baseCommand := tool.ReviveCommand
	if baseCommand == "" {
		baseCommand = tool.Command
	}
	if sess.AgentSessionID != "" && tool.ResumeByIDCommand != "" {
		baseCommand = strings.ReplaceAll(tool.ResumeByIDCommand, "{id}", sess.AgentSessionID)
	}
	command, env, err := m.buildLaunch(sess.Tool, tool, baseCommand, sess.ID)
	if err != nil {
		return err
	}
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		return err
	}
	if err := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); err != nil {
		return err
	}
	if err := m.store.UpdateStatus(sess.ID, tool.DefaultStatus); err != nil {
		return err
	}
	// The session is alive again; any watcher backoff from its dead spell
	// no longer applies.
	if m.focus != nil {
		m.focus.retryNow()
	}
	// A leftover ack from the previous life must not swallow the revived
	// agent's first finished alert.
	return m.store.SetAcked(sess.ID, false)
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
			m.err = err.Error()
			return m, nil
		}
		if len(live) == 0 {
			m.err = "no live sessions to kill in " + entry.group
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
		if !m.tmux.Exists(entry.sess.ID) {
			m.err = entry.sess.Name + " is already dead"
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionKill,
			sessions: []store.Session{entry.sess},
			label:    fmt.Sprintf("kill %s? frees its RAM, v revives it.", entry.sess.Name),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// killAllLive asks to end every live session in the current view, the
// batch counterpart to V.
func (m *Model) killAllLive() (tea.Model, tea.Cmd) {
	live, err := m.liveSessions(m.visibleSessions())
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if len(live) == 0 {
		m.err = "no live sessions to kill"
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
	panes, err := m.tmux.Panes()
	if err != nil {
		return nil, err
	}
	var live []store.Session
	for _, sess := range sessions {
		if panes[sess.ID] > 0 {
			live = append(live, sess)
		}
	}
	return live, nil
}

// killSession ends one session's tmux window, freeing everything its agent
// held, while the store row keeps the name, group, history and conversation
// id that revive needs. The pane is captured first so the preview still
// shows the agent's last output once the window is gone.
func (m *Model) killSession(sess store.Session) error {
	if !m.tmux.Exists(sess.ID) {
		return nil
	}
	if pane, err := m.tmux.CapturePane(sess.ID); err == nil && pane != "" {
		if err := m.setSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	var killErr error
	// Runs under the poller's lock so no pass can capture a half-killed
	// pane, and drops the pane hash the revived session would be compared
	// against.
	m.poller.reflowSessions([]string{sess.ID}, func() {
		killErr = m.tmux.Kill(sess.ID)
	})
	if killErr != nil {
		return killErr
	}
	// The agent dies without running its session-end hook, so a leftover
	// status file would otherwise decide what the revived session reads as.
	if err := m.hooks.Remove(sess.ID); err != nil {
		return err
	}
	if err := m.store.UpdateStatus(sess.ID, status.Dead); err != nil {
		return err
	}
	for i := range m.sessions {
		if m.sessions[i].ID == sess.ID {
			m.sessions[i].Status = status.Dead
		}
	}
	return nil
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionArchive,
			sessions: subtree,
			label:    fmt.Sprintf("archive group %s (%d sessions)?", entry.group, len(subtree)),
		}
	} else {
		m.confirm = confirmTarget{
			action:   actionArchive,
			sessions: []store.Session{entry.sess},
			label:    fmt.Sprintf("archive %s?", entry.sess.Name),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionRestore,
			sessions: subtree,
			label:    fmt.Sprintf("restore group %s (%d sessions)?", entry.group, len(subtree)),
		}
	} else {
		m.confirm = confirmTarget{
			action:   actionRestore,
			sessions: []store.Session{entry.sess},
			label:    fmt.Sprintf("restore %s?", entry.sess.Name),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// archivalSnapshot captures pane content for every still-live session
// in the confirm target, so the snapshot survives when the tmux window
// is later killed or the session window is reused for a new agent.
func (m *Model) archivalSnapshot() error {
	for _, sess := range m.confirm.sessions {
		if !m.tmux.Exists(sess.ID) {
			continue
		}
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil || pane == "" {
			continue
		}
		if err := m.setSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) applyConfirmedArchived(archived bool) error {
	if m.confirm.isGroup {
		return m.store.SetGroupArchived(m.confirm.path, archived)
	}
	return m.store.SetArchived(m.confirm.sessions[0].ID, archived)
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.err = "root is the top level; delete the sessions under it instead"
		return
	}
	if !entry.isGroup {
		m.confirm = confirmTarget{
			label:    "delete " + entry.sess.Name + "? kills its tmux session.",
			sessions: []store.Session{entry.sess},
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.store.SessionsInSubtree(entry.group)
	if err != nil {
		m.err = err.Error()
		return
	}
	if m.showArchived {
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
	for _, g := range m.groups {
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
	var archived []store.Session
	for _, sess := range subtree {
		if sess.Archived {
			archived = append(archived, sess)
		}
	}
	return confirmTarget{
		isGroup:      true,
		archivedOnly: true,
		path:         path,
		sessions:     archived,
		label: fmt.Sprintf("delete %s from the archive (%d archived sessions)? kills their tmux sessions, live ones stay.",
			path, len(archived)),
	}
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	defer func() { m.mode = modeList }()
	switch msg.String() {
	case "y", "enter":
		switch m.confirm.action {
		case actionArchive:
			if err := m.archivalSnapshot(); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if err := m.applyConfirmedArchived(true); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
		case actionRestore:
			if err := m.applyConfirmedArchived(false); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
		case actionKill:
			for _, sess := range m.confirm.sessions {
				if err := m.killSession(sess); err != nil {
					m.err = err.Error()
					return m, nil
				}
			}
			m.err = ""
			m.rebuildRows()
		case actionDelete:
			for _, sess := range m.confirm.sessions {
				if err := m.tmux.Kill(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				if err := m.hooks.Remove(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveName(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewRepo(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewBase(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewScope(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				delete(m.pickedRepos, sess.ID)
				if err := m.store.Delete(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
			}
			if m.confirm.isGroup {
				removed, err := m.deleteConfirmedGroups()
				if err != nil {
					m.err = err.Error()
					return m, nil
				}
				for _, path := range removed {
					delete(m.collapsed, path)
				}
				m.persistCollapsed()
			}
		default:
			m.err = fmt.Sprintf("unknown confirm action %q", m.confirm.action)
			return m, nil
		}
		m.confirm = confirmTarget{}
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}

// deleteConfirmedGroups removes the group rows the confirmed delete
// covers, reporting the paths that went so their fold state can go too.
func (m *Model) deleteConfirmedGroups() ([]string, error) {
	if m.confirm.archivedOnly {
		return m.store.PruneArchivedGroups(m.confirm.path)
	}
	return m.store.DeleteGroup(m.confirm.path)
}
