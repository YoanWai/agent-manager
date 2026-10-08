package ui

import (
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type lifecycleRequest struct {
	target        confirmTarget
	pane          sessioncmd.PaneSize
	watched       string
	allowLive     bool
	emptyNotice   string
	successNotice string
}

func (lifecycleRequest) effectRequest() {}

type relaunchedSession struct {
	id     string
	result sessioncmd.RelaunchResult
}
type lifecycleEffectResult struct {
	archive  sessioncmd.ArchiveResult
	deleted  sessioncmd.DeleteResult
	changed  []store.Session
	launches []relaunchedSession
	failed   []store.Session
	survivor bool
	notice   string
	warning  error
}

func (lifecycleEffectResult) effectResult() {}

func (m *Model) queueLifecycle(target confirmTarget, allowLive bool, emptyNotice string) {
	target.sessions = append([]store.Session(nil), target.sessions...)
	w, h := m.paneTargetSize()
	notice := ""
	if target.action == actionRevive && !target.batch && len(target.sessions) == 1 {
		notice = m.degradedResumeNotice(target.sessions[0])
	}
	m.enqueueEffect(lifecycleRequest{target: target, pane: sessioncmd.PaneSize{Width: w, Height: h}, watched: m.watchedSession(), allowLive: allowLive, emptyNotice: emptyNotice, successNotice: notice}, 0, false)
}

func (s effectServices) runLifecycle(request lifecycleRequest) (effectResult, error) {
	result := lifecycleEffectResult{}
	target, err := s.validateLifecycleSelection(request.target)
	if err != nil {
		return result, err
	}
	ids := make([]string, 0, len(target.sessions))
	for _, sess := range target.sessions {
		ids = append(ids, sess.ID)
	}
	var operationErr error
	reflowErr := s.reflow(ids, func() {
		if s.watch != nil && (target.action == actionArchive || target.action == actionDelete || target.action == actionKill || target.action == actionRestart) {
			for _, id := range ids {
				s.watch.unwatch(id)
			}
		}
		switch target.action {
		case actionArchive:
			result.archive, operationErr = s.lifecycle.ArchiveForHuman(sessioncmd.ArchiveSelection{Sessions: target.sessions, GroupPath: groupPath(target)})
		case actionRestore:
			wasDead := map[string]bool{}
			for _, sess := range target.sessions {
				wasDead[sess.ID] = !s.driver.Exists(sess.ID)
			}
			result.archive, operationErr = s.lifecycle.RestoreForHuman(sessioncmd.ArchiveSelection{Sessions: target.sessions, GroupPath: groupPath(target)}, request.pane)
			for _, sess := range result.archive.Sessions {
				if wasDead[sess.ID] {
					result.launches = append(result.launches, relaunchedSession{sess.ID, sessioncmd.RelaunchResult{LaunchedAt: sess.AgentLaunchedAt}})
				}
			}
			result.warning = result.archive.LabelError
		case actionDelete:
			result.deleted, operationErr = s.lifecycle.DeleteForHuman(sessioncmd.DeleteSelection{Sessions: target.sessions, GroupPath: groupPath(target), ArchivedOnly: target.archivedOnly})
		case actionKill, actionRestart, actionRevive:
			for i, sess := range target.sessions {
				var err error
				switch target.action {
				case actionKill:
					if !s.driver.Exists(sess.ID) {
						continue
					}
					var killed store.Session
					killed, err = s.lifecycle.Kill(sess)
					if err == nil {
						result.changed = append(result.changed, killed)
					}
				case actionRestart, actionRevive:
					if target.action == actionRevive && !request.allowLive && s.driver.Exists(sess.ID) {
						continue
					}
					var launched sessioncmd.RelaunchResult
					if target.action == actionRestart {
						launched, err = s.lifecycle.Restart(sess, request.pane)
					} else {
						launched, err = s.lifecycle.Revive(sess, request.pane)
					}
					if err == nil {
						result.launches = append(result.launches, relaunchedSession{sess.ID, launched})
						if launched.LabelError != nil {
							result.warning = launched.LabelError
						}
					}
				}
				if err != nil {
					if operationErr == nil {
						operationErr = err
					}
					if target.batch {
						result.failed = append(result.failed, sess)
						continue
					}
					result.failed = append(result.failed, target.sessions[i:]...)
					break
				}
			}
		default:
			operationErr = fmt.Errorf("unknown confirm action %q", target.action)
		}
		if request.watched != "" {
			result.survivor = s.driver.Exists(request.watched)
		}
	})
	if reflowErr != nil {
		return result, reflowErr
	}
	if target.action == actionRevive && !target.batch {
		result.notice = request.successNotice
	}
	if target.action == actionRevive && target.batch {
		switch {
		case operationErr != nil:
			result.notice = fmt.Sprintf("revived %d, first error: %s", len(result.launches), operationErr)
		case len(result.launches) == 0:
			result.notice = request.emptyNotice
		default:
			degraded := 0
			for _, each := range result.launches {
				if each.result.Degraded {
					degraded++
				}
			}
			if degraded > 0 {
				result.notice = fmt.Sprintf("revived %d, %d without a captured id (used --continue)", len(result.launches), degraded)
			}
		}
	}
	return result, operationErr
}

func (s effectServices) validateLifecycleSelection(target confirmTarget) (confirmTarget, error) {
	selection := target.selection
	if selection.kind == lifecycleSelectionNone {
		return target, nil
	}
	var (
		current []store.Session
		err     error
	)
	switch selection.kind {
	case lifecycleSelectionSession:
		root, getErr := s.store.Get(selection.rootID)
		if getErr != nil {
			return target, getErr
		}
		children, childrenErr := s.store.Children(selection.rootID)
		if childrenErr != nil {
			return target, childrenErr
		}
		current = append([]store.Session{root}, children...)
	case lifecycleSelectionGroup:
		current, err = s.store.SessionsInSubtree(target.path)
		if err != nil {
			return target, err
		}
	default:
		return target, fmt.Errorf("unknown lifecycle selection kind %d", selection.kind)
	}
	if selection.archivedOnly {
		current = archivedSessions(current)
	}
	if !sameSessionIDs(current, target.sessions) {
		return target, fmt.Errorf("sessions changed since confirmation; review the selection and try again")
	}
	target.sessions = current
	return target, nil
}

func sameSessionIDs(left, right []store.Session) bool {
	if len(left) != len(right) {
		return false
	}
	ids := make(map[string]bool, len(left))
	for _, sess := range left {
		ids[sess.ID] = true
	}
	for _, sess := range right {
		if !ids[sess.ID] {
			return false
		}
		delete(ids, sess.ID)
	}
	return len(ids) == 0
}

func (m *Model) applyLifecycleEffect(request lifecycleRequest, result lifecycleEffectResult, err error) tea.Cmd {
	target := request.target
	for _, sess := range append(result.changed, result.archive.Sessions...) {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == sess.ID {
				m.workspace.sessions[i].Status = sess.Status
			}
		}
	}
	if target.action == actionKill {
		for _, sess := range result.changed {
			m.forgetLaunch(sess.ID)
			delete(m.workspace.panes, sess.ID)
		}
	}
	group := ""
	if result.archive.GroupChanged {
		group = result.archive.GroupPath
	}
	switch target.action {
	case actionArchive, actionRestore:
		var durable []store.Session
		for _, sess := range result.archive.Sessions {
			if sess.Archived == (target.action == actionArchive) {
				durable = append(durable, sess)
				if sess.Archived {
					m.forgetLaunch(sess.ID)
				}
			}
		}
		if target.action == actionArchive {
			m.markArchivedLocally(durable, group)
		} else {
			m.markRestoredLocally(durable, group)
		}
	case actionDelete:
		for _, sess := range result.deleted.Deleted {
			delete(m.ledger.pickedRepos, sess.ID)
			delete(m.ledger.awaitedRenames, sess.ID)
			m.forgetLaunch(sess.ID)
			m.removeSessionLocally(sess.ID)
		}
		if len(result.deleted.RemovedGroups) > 0 {
			m.applyRailStateDecision(m.rail.ForgetGroups(result.deleted.RemovedGroups))
			m.pruneGroupsLocally(result.deleted.RemovedGroups)
		}
	}
	for _, each := range result.launches {
		m.markFreshPane(each.id)
		if m.ledger.launched == nil {
			m.ledger.launched = map[string]time.Time{}
		}
		m.ledger.launched[each.id] = time.Now()
		if target.action == actionRestart {
			m.bindRestartLocally(each.id, each.result.Conversation, each.result.LaunchedAt)
		} else {
			m.bindReviveLocally(each.id, each.result.LaunchedAt)
		}
	}
	if len(result.launches) > 0 && m.focusRuntime.watch != nil {
		m.focusRuntime.watch.retryNow()
	}
	m.rewatchSurvivor(request.watched, result.survivor)
	m.rebuildRows()
	m.errBar.text = result.notice
	if result.deleted.Notice != "" {
		m.errBar.text = result.deleted.Notice
	}
	if result.warning != nil && err == nil {
		m.errBar.text = result.warning.Error()
	}
	if err != nil {
		if target.batch {
			if result.notice == "" {
				m.errBar.text = err.Error()
			}
			return nil
		}
		if target.action == actionRestart || target.action == actionRevive || target.action == actionRestore {
			retry := request
			if len(result.failed) > 0 {
				retry.target.sessions = append([]store.Session(nil), result.failed...)
			}
			if target.action == actionRestore {
				done := map[string]bool{}
				for _, sess := range result.archive.Sessions {
					if !sess.Archived {
						done[sess.ID] = true
					}
				}
				retry.target.sessions = nil
				for _, sess := range target.sessions {
					if !done[sess.ID] {
						retry.target.sessions = append(retry.target.sessions, sess)
					}
				}
			}
			// An async completion must not steal a newly opened modal or its attachments.
			if m.mode == modeList {
				m.reportLaunchError(err)
				if m.mode == modeLaunchHint {
					m.launchFix.effectRetry = retry
				}
			} else {
				m.errBar.text = err.Error()
			}
		} else {
			m.errBar.text = err.Error()
		}
	}
	return nil
}

// rewatchSurvivor restarts the preview watcher a lifecycle worker stopped
// when the session it watched is still selected and still running.
func (m *Model) rewatchSurvivor(watched string, survivor bool) {
	if !survivor || m.focusRuntime.watch == nil {
		return
	}
	if sess, ok := m.selected(); ok && sess.ID == watched && !sess.Archived {
		m.watchSelection()
	}
}
