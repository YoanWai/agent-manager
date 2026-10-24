package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	sessiondialogs "github.com/YoanWai/agent-manager/internal/ui/session/dialogs"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Archive(m.lifecycleView(), row))
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Restore(m.lifecycleView(), row))
}

// markArchivedLocally flags the confirmed archive in the loaded rows, so they
// leave the active view on this frame instead of first showing the dead state
// their kill just gave them. A group archive also flags the group itself and
// every group under it, which is what hides the whole subtree from the active
// view.
func (m *Model) markArchivedLocally(edit *uilifecycle.ArchiveEdit) {
	changed := false
	for i := range m.workspace.sessions {
		sess := &m.workspace.sessions[i]
		if sess.Archived || !edit.Rows[sess.ID] {
			continue
		}
		sess.Archived = true
		m.ledger.lifecycle.MarkSession(sess.ID, uilifecycle.GoneMark{Archived: true})
		changed = true
	}
	if edit.Group != "" {
		for _, path := range append([]string{edit.Group}, uilifecycle.Subgroups(m.workspace.groups, edit.Group)...) {
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

// markRestoredLocally mirrors a completed restore in the loaded rows and group
// flags, so what came back changes views on this frame rather than waiting for
// the next poll. A group restore unarchives the subtree; a single restore also
// clears its group's ancestors, which is what the store write just did to keep
// a restored session under a live home.
func (m *Model) markRestoredLocally(edit *uilifecycle.ArchiveEdit, restored []store.Session) {
	for i := range m.workspace.sessions {
		sess := &m.workspace.sessions[i]
		if !sess.Archived {
			continue
		}
		if edit.Rows[sess.ID] || (edit.Group != "" && uirail.InGroupSubtree(sess.Group, edit.Group)) {
			sess.Archived = false
			m.ledger.lifecycle.MarkSession(sess.ID, uilifecycle.GoneMark{Archived: false})
		}
	}
	if edit.Group != "" {
		for _, path := range append([]string{edit.Group}, uilifecycle.Subgroups(m.workspace.groups, edit.Group)...) {
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

func groupPath(confirm confirmTarget) string {
	if confirm.isGroup {
		return confirm.path
	}
	return ""
}

func (m *Model) queueLifecycle(card sessiondialogs.Card, allowLive bool, emptyNotice string) {
	target := cardTarget(card)
	target.sessions = append([]store.Session(nil), target.sessions...)
	w, h := m.paneTargetSize()
	m.enqueueEffect(uilifecycle.Request{Target: packageTarget(target), Pane: sessioncmd.PaneSize{Width: w, Height: h}, Watched: m.focus.runtime.Watching(), AllowLive: allowLive, EmptyNotice: emptyNotice}, 0, false)
}

// applyLifecycleEffect reconciles the loaded list with what a session action
// did: the statuses that changed, the rows that left or came back, the panes
// this run launched, and what the completion reports.
func (m *Model) applyLifecycleEffect(request uilifecycle.Request, result uilifecycle.Result, err error) tea.Cmd {
	change := uilifecycle.Changes(request, result, err)
	for _, each := range change.Statuses {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == each.ID {
				m.workspace.sessions[i].Status = each.Status
			}
		}
	}
	for _, id := range change.Forget {
		m.forgetLaunch(id)
		delete(m.workspace.panes, id)
	}
	if edit := change.Edit; edit != nil {
		if edit.Archive {
			m.markArchivedLocally(edit)
		} else {
			m.markRestoredLocally(edit, result.Archive.Sessions)
		}
	}
	for _, sess := range change.Deleted {
		m.ledger.lifecycle.ForgetSession(sess.ID)
		m.removeSessionLocally(sess.ID)
	}
	if len(change.RemovedGroups) > 0 {
		m.queueRail(m.rail.ForgetGroups(change.RemovedGroups).Mutations, 0, true)
		m.pruneGroupsLocally(change.RemovedGroups)
	}
	for _, each := range change.Launches {
		m.markFreshPane(each.ID)
		m.ledger.lifecycle.RecordLaunch(each.ID, time.Now())
		if confirmAction(request.Target.Action) == uidialogs.ConfirmRestart {
			m.bindRestartLocally(each.ID, each.Result.Conversation, each.Result.LaunchedAt)
		} else {
			m.bindReviveLocally(each.ID, each.Result.LaunchedAt)
		}
	}
	if len(change.Launches) > 0 {
		m.focus.runtime.Retry()
	}
	m.rewatchSurvivor(request.Watched, change.Survivor)
	m.rebuildRows()
	for _, notice := range change.Notices {
		m.reportErr(notice)
	}
	if change.Warning != nil {
		m.reportErr(change.Warning.Error())
	}
	m.reportRefusal(change.Refusal)
	return nil
}

// reportRefusal reports the failure that stopped a run: a batch names it only
// when the run said nothing else, and a single relaunch routes it to the fix
// card instead of the error bar.
func (m *Model) reportRefusal(refusal *uilifecycle.Refusal) {
	if refusal == nil {
		return
	}
	if !refusal.ViaLaunchHint {
		m.reportErr(refusal.Err.Error())
		return
	}
	// An async completion must not steal a newly opened modal or its
	// attachments.
	if !m.listClear() {
		m.reportErr(refusal.Err.Error())
		return
	}
	m.reportLaunchError(refusal.Err)
	if refusal.Retry == nil {
		return
	}
	if fix, ok := m.launchHintFix(); ok {
		fix.Retry = *refusal.Retry
	}
}

// rewatchSurvivor restarts the preview watcher a lifecycle worker stopped when
// the session it watched is still selected and still running.
func (m *Model) rewatchSurvivor(watched string, survivor bool) {
	if !survivor {
		return
	}
	if sess, ok := m.selected(); ok && sess.ID == watched && !sess.Archived {
		m.watchSelection()
	}
}
