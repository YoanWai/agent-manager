package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
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
		m.reportErr("root is the top level, not a group to archive")
		return m, nil
	}
	var target confirmTarget
	if entry.isGroup {
		subtree := m.sessionsInSubtree(entry.group)
		target = confirmTarget{
			isGroup:   true,
			path:      entry.group,
			action:    uidialogs.ConfirmArchive,
			sessions:  subtree,
			label:     fmt.Sprintf("archive group %s (%d sessions)? frees their RAM, t to find them.", entry.group, len(subtree)),
			selection: lifecycleSelection{kind: lifecycleSelectionGroup},
		}
	} else {
		sessions := m.sessionAndChildren(entry.sess)
		target = confirmTarget{
			action:   uidialogs.ConfirmArchive,
			sessions: sessions,
			label: uisession.FollowConfirmLabel("archive", entry.sess.Name, len(sessions)-1,
				"frees its RAM, t to find it.",
				"frees their RAM, t to find them."),
			selection: lifecycleSelection{kind: lifecycleSelectionSession, rootID: entry.sess.ID},
		}
	}
	m.openConfirm(target)
	m.clearErr()
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
		m.reportErr("root is the top level, not a group to restore")
		return m, nil
	}
	var target confirmTarget
	if entry.isGroup {
		subtree := m.sessionsInSubtree(entry.group)
		archived := archivedSessions(subtree)
		target = confirmTarget{
			isGroup:   true,
			path:      entry.group,
			action:    uidialogs.ConfirmRestore,
			sessions:  archived,
			label:     fmt.Sprintf("restore group %s (%d archived sessions)? brings them back.", entry.group, len(archived)),
			selection: lifecycleSelection{kind: lifecycleSelectionGroup, archivedOnly: true},
		}
	} else {
		sessions := m.sessionAndChildren(entry.sess)
		sessions = archivedSessions(sessions)
		target = confirmTarget{
			action:   uidialogs.ConfirmRestore,
			sessions: sessions,
			label: uisession.FollowConfirmLabel("restore", entry.sess.Name, len(sessions)-1,
				"brings it back.",
				"brings them back."),
			selection: lifecycleSelection{kind: lifecycleSelectionSession, rootID: entry.sess.ID, archivedOnly: true},
		}
	}
	m.openConfirm(target)
	m.clearErr()
	return m, nil
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
			m.ledger.lifecycle.MarkSession(sess.ID, uilifecycle.GoneMark{Archived: true})
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
			m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Archived: true})
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
		if byID[m.workspace.sessions[i].ID] || (groupPath != "" && uirail.InGroupSubtree(m.workspace.sessions[i].Group, groupPath)) {
			m.workspace.sessions[i].Archived = false
			m.ledger.lifecycle.MarkSession(m.workspace.sessions[i].ID, uilifecycle.GoneMark{Archived: false})
		}
	}
	if groupPath != "" {
		for _, path := range append([]string{groupPath}, m.subgroupPaths(groupPath)...) {
			delete(m.workspace.archivedGroups, path)
			m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Archived: false})
		}
	} else {
		for _, sess := range restored {
			for path := sess.Group; path != ""; path = uirail.ParentGroup(path) {
				delete(m.workspace.archivedGroups, path)
				m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Archived: false})
			}
		}
	}
	m.rebuildRows()
}

func (m *Model) sessionAndChildren(sess store.Session) []store.Session {
	out := make([]store.Session, 0, 1)
	out = append(out, sess)
	for _, candidate := range m.workspace.sessions {
		if candidate.ParentID == sess.ID {
			out = append(out, candidate)
		}
	}
	return out
}

func (m *Model) sessionsInSubtree(path string) []store.Session {
	var sessions []store.Session
	for _, sess := range m.workspace.sessions {
		if uirail.InGroupSubtree(sess.Group, path) {
			sessions = append(sessions, sess)
		}
	}
	return sessions
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

func (m *Model) queueLifecycle(target confirmTarget, allowLive bool, emptyNotice string) {
	target.sessions = append([]store.Session(nil), target.sessions...)
	w, h := m.paneTargetSize()
	m.enqueueEffect(uilifecycle.Request{Target: packageTarget(target), Pane: sessioncmd.PaneSize{Width: w, Height: h}, Watched: m.focus.runtime.Watching(), AllowLive: allowLive, EmptyNotice: emptyNotice}, 0, false)
}

// applyLifecycleEffect reconciles the loaded list with what a session action
// did: the statuses that changed, the rows that left or came back, the panes
// this run launched, and what the completion reports.
func (m *Model) applyLifecycleEffect(request uilifecycle.Request, result uilifecycle.Result, err error) tea.Cmd {
	target := confirmTargetFor(request.Target)
	for _, sess := range append(result.Changed, result.Archive.Sessions...) {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == sess.ID {
				m.workspace.sessions[i].Status = sess.Status
			}
		}
	}
	if target.action == uidialogs.ConfirmKill {
		for _, sess := range result.Changed {
			m.forgetLaunch(sess.ID)
			delete(m.workspace.panes, sess.ID)
		}
	}
	group := ""
	if result.Archive.GroupChanged {
		group = result.Archive.GroupPath
	}
	switch target.action {
	case uidialogs.ConfirmArchive, uidialogs.ConfirmRestore:
		var durable []store.Session
		for _, sess := range result.Archive.Sessions {
			if sess.Archived == (target.action == uidialogs.ConfirmArchive) {
				durable = append(durable, sess)
				if sess.Archived {
					m.forgetLaunch(sess.ID)
				}
			}
		}
		if target.action == uidialogs.ConfirmArchive {
			m.markArchivedLocally(durable, group)
		} else {
			m.markRestoredLocally(durable, group)
		}
	case uidialogs.ConfirmDelete:
		for _, sess := range result.Deleted.Deleted {
			m.ledger.lifecycle.ForgetSession(sess.ID)
			m.removeSessionLocally(sess.ID)
		}
		if len(result.Deleted.RemovedGroups) > 0 {
			m.queueRail(m.rail.ForgetGroups(result.Deleted.RemovedGroups), 0, true)
			m.pruneGroupsLocally(result.Deleted.RemovedGroups)
		}
	}
	for _, each := range result.Launches {
		m.markFreshPane(each.ID)
		m.ledger.lifecycle.RecordLaunch(each.ID, time.Now())
		if target.action == uidialogs.ConfirmRestart {
			m.bindRestartLocally(each.ID, each.Result.Conversation, each.Result.LaunchedAt)
		} else {
			m.bindReviveLocally(each.ID, each.Result.LaunchedAt)
		}
	}
	if len(result.Launches) > 0 {
		m.focus.runtime.Retry()
	}
	m.rewatchSurvivor(request.Watched, result.Survivor)
	m.rebuildRows()
	m.reportErr(result.Notice)
	if result.Deleted.Notice != "" {
		m.reportErr(result.Deleted.Notice)
	}
	if result.Warning != nil && err == nil {
		m.reportErr(result.Warning.Error())
	}
	if err != nil {
		if target.batch {
			if result.Notice == "" {
				m.reportErr(err.Error())
			}
			return nil
		}
		if target.action == uidialogs.ConfirmRestart || target.action == uidialogs.ConfirmRevive || target.action == uidialogs.ConfirmRestore {
			retry := request
			if len(result.Failed) > 0 {
				retry.Target.Sessions = append([]store.Session(nil), result.Failed...)
			}
			if target.action == uidialogs.ConfirmRestore {
				done := map[string]bool{}
				for _, sess := range result.Archive.Sessions {
					if !sess.Archived {
						done[sess.ID] = true
					}
				}
				retry.Target.Sessions = nil
				for _, sess := range target.sessions {
					if !done[sess.ID] {
						retry.Target.Sessions = append(retry.Target.Sessions, sess)
					}
				}
			}
			// An async completion must not steal a newly opened modal or its attachments.
			if m.listClear() {
				m.reportLaunchError(err)
				if fix, ok := m.launchHintFix(); ok {
					fix.Retry = retry
				}
			} else {
				m.reportErr(err.Error())
			}
		} else {
			m.reportErr(err.Error())
		}
	}
	return nil
}

// rewatchSurvivor restarts the preview watcher a lifecycle worker stopped
// when the session it watched is still selected and still running.
func (m *Model) rewatchSurvivor(watched string, survivor bool) {
	if !survivor {
		return
	}
	if sess, ok := m.selected(); ok && sess.ID == watched && !sess.Archived {
		m.watchSelection()
	}
}
