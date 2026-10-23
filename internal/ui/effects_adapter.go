package ui

import (
	"maps"
	"slices"
	"time"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// effectState is the root's queue plus the fence a poll reads: a listing
// captured before the latest mutation finished is stale.
type effectState struct {
	uieffects.Queue
	latestObservation time.Time
}

func (m *Model) laneFor(request uieffects.Request) *uieffects.Lane {
	switch request.(type) {
	case inputRequest:
		return &m.effects.Input
	case remoteRequest:
		return &m.effects.Remote
	}
	return &m.effects.Main
}

// effectStopWait bounds shutdown: an effect stuck on a hook or an unanswered
// prompt must not keep the process alive after its terminal is gone.
var effectStopWait = 5 * time.Second

// StopEffects refuses new effects and waits briefly for running ones. A quit
// the user forced, or work that outlives the wait, is recorded in
// AbandonedEffects.
func (m *Model) StopEffects() { m.effects.Stop(effectStopWait, effectName) }

// AbandonedEffects lists effects whose outcome the manager could not confirm
// before it exited.
func (m *Model) AbandonedEffects() []string { return slices.Clone(m.effects.Abandoned) }

func (m *Model) enqueueEffect(request uieffects.Request, chain uint64, first bool) {
	if m.effects.Quitting && !first {
		return
	}
	switch value := request.(type) {
	case inputRequest:
		value.args = slices.Clone(value.args)
		value.session.PendingInputs = slices.Clone(value.session.PendingInputs)
		value.session.RelaunchSnapshot = maps.Clone(value.session.RelaunchSnapshot)
		request = value
	case quickSendRequest:
		value.session.PendingInputs = slices.Clone(value.session.PendingInputs)
		value.session.RelaunchSnapshot = maps.Clone(value.session.RelaunchSnapshot)
		value.images = slices.Clone(value.images)
		request = value
	case installStartRequest:
		value.images = slices.Clone(value.images)
		request = value
	case installSettleRequest:
		value.install.images = slices.Clone(value.install.images)
		request = value
	case lifecycleRequest:
		value.target.sessions = slices.Clone(value.target.sessions)
		for i := range value.target.sessions {
			value.target.sessions[i].PendingInputs = slices.Clone(value.target.sessions[i].PendingInputs)
			value.target.sessions[i].RelaunchSnapshot = maps.Clone(value.target.sessions[i].RelaunchSnapshot)
		}
		request = value
	case railRequest:
		value.mutation.Collapsed = slices.Clone(value.mutation.Collapsed)
		value.mutation.HostCollapsed = slices.Clone(value.mutation.HostCollapsed)
		value.mutation.GroupSiblings = slices.Clone(value.mutation.GroupSiblings)
		request = value
	case forkRequest:
		value.source.PendingInputs = slices.Clone(value.source.PendingInputs)
		value.source.RelaunchSnapshot = maps.Clone(value.source.RelaunchSnapshot)
		request = value
	case spawnRequest:
		value.dirFallbacks = slices.Clone(value.dirFallbacks)
		value.terminalDir.fallbacks = slices.Clone(value.terminalDir.fallbacks)
		if value.plan != nil {
			plan := *value.plan
			plan.PendingInputs = slices.Clone(plan.PendingInputs)
			value.plan = &plan
		}
		value.images = slices.Clone(value.images)
		request = value
	case groupRequest:
		value.fallbacks = slices.Clone(value.fallbacks)
		request = value
	case renameRequest:
		value.sess.PendingInputs = slices.Clone(value.sess.PendingInputs)
		value.sess.RelaunchSnapshot = maps.Clone(value.sess.RelaunchSnapshot)
		request = value
	case settingsRequest:
		value.values = slices.Clone(value.values)
		value.hidden = slices.Clone(value.hidden)
		request = value
	case geometryRequest:
		value.targets = slices.Clone(value.targets)
		request = value
	case reviewEffectRequest:
		value.state.Reviewed = maps.Clone(value.state.Reviewed)
		value.state.Comments = slices.Clone(value.state.Comments)
		value.previousState.Reviewed = maps.Clone(value.previousState.Reviewed)
		value.previousState.Comments = slices.Clone(value.previousState.Comments)
		value.commentIDs = slices.Clone(value.commentIDs)
		request = value
	}
	m.effects.Push(m.laneFor(request), request, chain, first, m.captureEffect(request))
}

func (m *Model) nextEffectCmd() tea.Cmd { return m.effects.Next(m.heldBy) }

// heldSessions names the panes a main-lane job kills or relaunches: the
// sessions a lifecycle action targets, and a session ending after its turn
// together with the terminals nested under it.
func (m *Model) heldSessions(request uieffects.Request) []string {
	switch value := request.(type) {
	case lifecycleRequest:
		ids := make([]string, 0, len(value.target.sessions))
		for _, sess := range value.target.sessions {
			ids = append(ids, sess.ID)
		}
		return ids
	case afterTurnRequest:
		if value.cancel {
			return nil
		}
		ids := []string{value.id}
		for _, sess := range m.workspace.sessions {
			if sess.ParentID == value.id {
				ids = append(ids, sess.ID)
			}
		}
		return ids
	}
	return nil
}

func inputTarget(job *uieffects.Job) (string, bool) {
	if job == nil {
		return "", false
	}
	request, ok := job.Request.(inputRequest)
	return request.session.ID, ok
}

// heldBy reports whether the main-lane job kills or relaunches the pane the
// input job writes to.
func (m *Model) heldBy(main, input *uieffects.Job) bool {
	id, ok := inputTarget(input)
	return ok && slices.Contains(m.heldSessions(main.Request), id)
}

func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if install := m.pendingInstall(); install != nil {
		m.reportErr("install may still be running in " + install.name + "; finish it, kill its terminal with the session controls, or attach and interrupt it before quitting")
		return m, nil
	}
	starting := func(job *uieffects.Job) (installStartRequest, bool) {
		if job == nil {
			return installStartRequest{}, false
		}
		request, ok := job.Request.(installStartRequest)
		return request, ok
	}
	if request, ok := starting(m.effects.Main.Active); ok {
		m.reportErr("install for " + request.binary + " is still starting; wait for its installer shell before quitting")
		return m, nil
	}
	for _, job := range m.effects.Main.Pending {
		if request, ok := starting(job); ok {
			m.reportErr("install for " + request.binary + " is still queued; wait for its installer shell before quitting")
			return m, nil
		}
	}
	if m.effects.Quitting {
		return m, m.effects.Abandon(effectName)
	}
	m.prepareSplitForQuit()
	m.effects.Quitting = true
	if awaited, ok := m.effects.FirstUnfinished(); ok {
		m.reportErr("finishing " + effectName(awaited.Request) + " before quitting; press the quit key again to leave without waiting")
	}
	return m, m.nextEffectCmd()
}

func (m *Model) handleEffectCompleted(msg uieffects.Completed) (tea.Model, tea.Cmd) {
	job := m.effects.Finish(msg.ID)
	if job == nil {
		return m, nil
	}
	// Keys, paste and mouse change no stored row, so they neither fence a
	// listing nor ask for a poll; a typed prompt records LastPrompt and does.
	if request, ok := job.Request.(inputRequest); ok && request.kind != inputPrompt {
		result, _ := msg.Result.(inputEffectResult)
		m.applyInputEffect(result, msg.Err)
		return m, m.nextEffectCmd()
	}
	switch job.Request.(type) {
	case inputRequest, quickSendRequest, installStartRequest, lifecycleRequest, afterTurnRequest, railRequest, forkRequest, spawnRequest, groupRequest, renameRequest, focusRequest, ackRequest, attachRequest:
		if msg.FinishedAt.After(m.effects.latestObservation) {
			m.effects.latestObservation = msg.FinishedAt
		}
	case reviewEffectRequest:
		// A send types into the agent pane, so a poll captured before it
		// is stale exactly like a lifecycle mutation; reads are not.
		if job.Request.(reviewEffectRequest).op == reviewOpSend && msg.FinishedAt.After(m.effects.latestObservation) {
			m.effects.latestObservation = msg.FinishedAt
		}
	}
	var command tea.Cmd
	switch result := msg.Result.(type) {
	case inputEffectResult:
		m.applyInputEffect(result, msg.Err)
	case quickSendResult:
		command = m.applyQuickSend(job.Request.(quickSendRequest), result, msg.Err)
	case noticeDismissResult:
		m.applyNoticeDismiss(job, msg.Err)
	case splitSaveResult:
		m.applySplitSave(job, msg.Err)
	case installStartResult:
		command = m.applyInstallStart(job.Request.(installStartRequest), result, msg.Err)
	case installSettleResult:
		command = m.applyInstallSettle(job.Request.(installSettleRequest), result, msg.Err)
	case lifecycleEffectResult:
		command = m.applyLifecycleEffect(job.Request.(lifecycleRequest), result, msg.Err)
	case afterTurnResult:
		command = m.applyAfterTurn(job.Request.(afterTurnRequest), result, msg.Err)
	case railEffectResult:
		command = m.applyRailEffect(job, result, msg.Err)
	case forkEffectResult:
		command = m.applyForkEffect(job.Request.(forkRequest), result, msg.Err)
	case geometryEffectResult:
		m.applyGeometryEffect(result, msg.Err)
	case spawnEffectResult:
		command = m.applySpawnEffect(job.Request.(spawnRequest), result, msg.Err)
	case groupEffectResult:
		command = m.applyGroupEffect(job.Request.(groupRequest), result, msg.Err)
	case renameEffectResult:
		m.applyRenameEffect(job, result, msg.Err)
	case moveDialogCloseResult:
		m.applyMoveDialogClose(job.Request.(moveDialogClose))
	case settingsEffectResult:
		command = m.applySettingsEffect(job, result, msg.Err)
	case savedPromptResult:
		m.applySavedPromptEffect(job.Request.(savedPromptRequest), result, msg.Err)
	case reviewEffectResult:
		command = m.applyReviewEffect(job, result, msg.Err)
	case keysEffectResult:
		command = m.applyKeysEffect(job, result, msg.Err)
	case focusEffectResult:
		command = m.applyFocusEffect(job.Request.(focusRequest), result, msg.Err)
	case ackEffectResult:
		m.applyAckEffect(job, result, msg.Err)
	case detachEffectResult:
		command = m.applyDetachEffect(job.Request.(detachRequest), result, msg.Err)
	case attachEffectResult:
		command = m.applyAttachEffect(job.Request.(attachRequest), result, msg.Err)
	case connectionEffectResult:
		command = m.applyConnectionEffect(job.Request.(connectionRequest), result, msg.Err)
	case remoteEffectResult:
		command = m.applyRemoteEffect(job.Request.(remoteRequest), result, msg.Err)
	default:
		if msg.Err != nil {
			m.reportErr(msg.Err.Error())
		}
	}
	// A settle check runs once per poll; asking for a poll after it would
	// spin the poller for as long as the installer runs.
	if _, settling := job.Request.(installSettleRequest); m.poller != nil && !settling {
		m.requestRefresh()
	}
	return m, tea.Batch(command, m.nextEffectCmd())
}

func effectName(request uieffects.Request) string {
	switch request := request.(type) {
	case inputRequest:
		return "input to a session"
	case quickSendRequest:
		return "quick send"
	case noticeDismissRequest:
		return "notice dismissal"
	case splitSaveRequest:
		return "split ratio save"
	case choiceSaveRequest:
		return "model choice save"
	case installStartRequest, installSettleRequest:
		return "CLI install"
	case lifecycleRequest:
		if request.target.action == actionDelete {
			return "session delete"
		}
		return "session " + string(request.target.action)
	case afterTurnRequest:
		return "after-turn archive or kill"
	case railRequest:
		return "rail change"
	case forkRequest:
		return "fork"
	case geometryRequest:
		return "pane resize"
	case spawnRequest:
		return "spawn"
	case groupRequest:
		return "group creation"
	case renameRequest:
		return "rename"
	case moveDialogClose:
		return "move"
	case settingsRequest:
		return "settings save"
	case savedPromptRequest:
		return "saved prompt change"
	case reviewEffectRequest:
		return "review change"
	case keysRequest:
		return "key binding save"
	case focusRequest, ackRequest, detachRequest, attachRequest:
		return "focus or attach"
	case connectionRequest:
		return "SSH connection save"
	case remoteRequest:
		return "call to an SSH connection"
	}
	return "effect"
}

func (m *Model) routeEffectMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case uieffects.Completed:
		return routed(m.handleEffectCompleted(msg))
	}
	return nil, nil, false
}
