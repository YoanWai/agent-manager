package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	if m.rail.ShowArchived() {
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
	if !m.rail.ShowArchived() {
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
	defer m.restoreSurvivingWatcher(m.watchedSession())

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
	if revived && m.focusRuntime.watch != nil {
		m.focusRuntime.watch.retryNow()
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

func archivedSessions(sessions []store.Session) []store.Session {
	var archived []store.Session
	for _, sess := range sessions {
		if sess.Archived {
			archived = append(archived, sess)
		}
	}
	return archived
}
