package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
	"maps"
	"slices"
)

// lifecycleRequest is the root's queue shape of a session action; the work
// itself runs in ui/session/lifecycle.
type lifecycleRequest struct {
	target      confirmTarget
	pane        sessioncmd.PaneSize
	watched     string
	allowLive   bool
	emptyNotice string
}

func (request lifecycleRequest) EffectName() string {
	if request.target.action == uidialogs.ConfirmDelete {
		return "session delete"
	}
	return "session " + string(request.target.action)
}

func (value lifecycleRequest) EffectCopy() uieffects.Request {
	value.target.sessions = slices.Clone(value.target.sessions)
	for i := range value.target.sessions {
		value.target.sessions[i].PendingInputs = slices.Clone(value.target.sessions[i].PendingInputs)
		value.target.sessions[i].RelaunchSnapshot = maps.Clone(value.target.sessions[i].RelaunchSnapshot)
	}
	return value
}

func (request lifecycleRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runLifecycle(request)
}

type lifecycleEffectResult struct{ uilifecycle.Result }

func (lifecycleEffectResult) EffectResult() {}

func (result lifecycleEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyLifecycleEffect(job.Request.(lifecycleRequest), result, err)
}

func (m *Model) queueLifecycle(target confirmTarget, allowLive bool, emptyNotice string) {
	target.sessions = append([]store.Session(nil), target.sessions...)
	w, h := m.paneTargetSize()
	m.enqueueEffect(lifecycleRequest{target: target, pane: sessioncmd.PaneSize{Width: w, Height: h}, watched: m.focus.runtime.Watching(), allowLive: allowLive, emptyNotice: emptyNotice}, 0, false)
}

func (s effectServices) runLifecycle(request lifecycleRequest) (uieffects.Result, error) {
	result, err := uilifecycle.Run(s.lifecycleServices(), uilifecycle.Request{
		Target:      packageTarget(request.target),
		Pane:        request.pane,
		Watched:     request.watched,
		AllowLive:   request.allowLive,
		EmptyNotice: request.emptyNotice,
	})
	return lifecycleEffectResult{result}, err
}

// lifecycleServices is the effect services as a lifecycle run reaches them.
func (s effectServices) lifecycleServices() uilifecycle.Services {
	services := uilifecycle.Services{
		Lifecycle: s.lifecycle,
		Store:     s.store,
		Driver:    s.driver,
		Reflow:    s.reflowSessions,
	}
	// A nil watcher stored in the interface would read as present.
	if s.watch != nil {
		services.Watch = s.watch
	}
	return services
}

// reflowSessions holds the panes a lifecycle run touches, the lane contract
// that keeps an input job from writing into a session it is killing.
func (s effectServices) reflowSessions(ids []string, work func()) error { return s.reflow(ids, work) }

// packageTarget is a confirmation as the lifecycle package runs it.
func packageTarget(target confirmTarget) uilifecycle.Target {
	return uilifecycle.Target{
		Action:       uilifecycle.Action(target.action),
		Sessions:     target.sessions,
		Path:         groupPath(target),
		Batch:        target.batch,
		ArchivedOnly: target.archivedOnly,
		Selection:    packageSelection(target.selection),
	}
}

// confirmTargetFor is a lifecycle the package decided on its own, an after-turn
// archive or kill, in the shape the confirmation's apply reads.
func confirmTargetFor(target uilifecycle.Target) confirmTarget {
	return confirmTarget{
		isGroup:      target.Path != "",
		path:         target.Path,
		sessions:     target.Sessions,
		action:       confirmAction(target.Action),
		batch:        target.Batch,
		archivedOnly: target.ArchivedOnly,
	}
}

// confirmAction is the package's action as the card spells it: the card
// names delete with its zero value.
func confirmAction(action uilifecycle.Action) uidialogs.ConfirmAction {
	if action == uilifecycle.Remove {
		return uidialogs.ConfirmDelete
	}
	return uidialogs.ConfirmAction(action)
}

func packageSelection(selection lifecycleSelection) uilifecycle.Selection {
	return uilifecycle.Selection{Kind: uilifecycle.SelectionKind(selection.kind), RootID: selection.rootID, ArchivedOnly: selection.archivedOnly}
}

func (m *Model) applyLifecycleEffect(request lifecycleRequest, result lifecycleEffectResult, err error) tea.Cmd {
	target := request.target
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
	m.rewatchSurvivor(request.watched, result.Survivor)
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
				retry.target.sessions = append([]store.Session(nil), result.Failed...)
			}
			if target.action == uidialogs.ConfirmRestore {
				done := map[string]bool{}
				for _, sess := range result.Archive.Sessions {
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
