package ui

import (
	"slices"
	"time"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	tea "github.com/charmbracelet/bubbletea"
)

// effectRequest is a request still declared at the root: it runs against the
// root's own services. A request a feature package declares runs against that
// package's services instead, through bindEffect.
type effectRequest interface {
	uieffects.Request
	run(effectServices) (uieffects.Result, error)
}

// effectResult is a result still declared at the root, which applies itself.
// A package result is applied by applyEffect.
type effectResult interface {
	uieffects.Result
	apply(m *Model, job *uieffects.Job, err error) tea.Cmd
}

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
	request = request.EffectCopy()
	m.effects.Push(m.laneFor(request), request, chain, first, m.bindEffect(request))
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
	return request.request.Session.ID, ok
}

// heldBy reports whether the main-lane job kills or relaunches the pane the
// input job writes to.
func (m *Model) heldBy(main, input *uieffects.Job) bool {
	id, ok := inputTarget(input)
	return ok && slices.Contains(m.heldSessions(main.Request), id)
}

func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if install := m.ledger.install.Pending; install != nil {
		m.reportErr("install may still be running in " + install.Name + "; finish it, kill its terminal with the session controls, or attach and interrupt it before quitting")
		return m, nil
	}
	starting := func(job *uieffects.Job) (uiinstall.StartRequest, bool) {
		if job == nil {
			return uiinstall.StartRequest{}, false
		}
		request, ok := job.Request.(uiinstall.StartRequest)
		return request, ok
	}
	if request, ok := starting(m.effects.Main.Active); ok {
		m.reportErr("install for " + request.Binary + " is still starting; wait for its installer shell before quitting")
		return m, nil
	}
	for _, job := range m.effects.Main.Pending {
		if request, ok := starting(job); ok {
			m.reportErr("install for " + request.Binary + " is still queued; wait for its installer shell before quitting")
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
	if request, ok := job.Request.(inputRequest); ok && request.request.Kind != focusruntime.Prompt {
		result, _ := msg.Result.(inputEffectResult)
		m.applyInputEffect(result, msg.Err)
		return m, m.nextEffectCmd()
	}
	switch job.Request.(type) {
	case inputRequest, quickSendRequest, uiinstall.StartRequest, lifecycleRequest, afterTurnRequest, railRequest, forkRequest, spawnRequest, groupRequest, renameRequest, focusRequest, ackRequest, attachRequest:
		if msg.FinishedAt.After(m.effects.latestObservation) {
			m.effects.latestObservation = msg.FinishedAt
		}
	case uireviewsource.Change:
		// A send types into the agent pane, so a poll captured before it
		// is stale exactly like a lifecycle mutation; reads are not.
		if uireviewsource.IsSend(job.Request) && msg.FinishedAt.After(m.effects.latestObservation) {
			m.effects.latestObservation = msg.FinishedAt
		}
	}
	command := m.applyEffect(job, msg.Result, msg.Err)
	// A settle check runs once per poll; asking for a poll after it would
	// spin the poller for as long as the installer runs.
	if _, settling := job.Request.(uiinstall.SettleRequest); m.poller != nil && !settling {
		m.requestRefresh()
	}
	return m, tea.Batch(command, m.nextEffectCmd())
}

func effectName(request uieffects.Request) string { return request.EffectName() }

func (m *Model) routeEffectMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case uieffects.Completed:
		return routed(m.handleEffectCompleted(msg))
	}
	return nil, nil, false
}
