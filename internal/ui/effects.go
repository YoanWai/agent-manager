package ui

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var errEffectsStopped = errors.New("UI effects have stopped")

type effectRequest interface{ effectRequest() }
type effectResult interface{ effectResult() }

type effectCompletedMsg struct {
	id         uint64
	result     effectResult
	err        error
	finishedAt time.Time
}

type effectJob struct {
	id, chain uint64
	request   effectRequest
	command   tea.Cmd
}

// Effects run on two FIFO lanes. Input forwarded to a pane has its own lane
// so a keystroke never waits behind a spawn, fork or poll-locked reflow; each
// input re-checks its session's identity in the worker, so it needs no order
// against lifecycle work.
type effectState struct {
	nextID, nextChain uint64
	main, input       effectLane
	lifetime          *effectLifetime
	latestObservation time.Time
	quitting          bool
	abandoned         []string
}

type effectLane struct {
	active  *effectJob
	pending []*effectJob
}

func (m *Model) laneFor(request effectRequest) *effectLane {
	if _, ok := request.(inputRequest); ok {
		return &m.effects.input
	}
	return &m.effects.main
}

type effectLifetime struct {
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

func (l *effectLifetime) begin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.running.Add(1)
	return true
}

// closeAndWait refuses new work and waits up to limit for running work,
// reporting whether it all finished.
func (l *effectLifetime) closeAndWait(limit time.Duration) bool {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	finished := make(chan struct{})
	go func() {
		l.running.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(limit):
		return false
	}
}

// effectStopWait bounds shutdown: an effect stuck on a hook or an unanswered
// prompt must not keep the process alive after its terminal is gone.
var effectStopWait = 5 * time.Second

// StopEffects refuses new effects and waits briefly for running ones. A quit
// the user forced, or work that outlives the wait, is recorded in
// AbandonedEffects.
func (m *Model) StopEffects() {
	if m.effects.lifetime == nil {
		return
	}
	limit := effectStopWait
	if len(m.effects.abandoned) > 0 {
		limit = 0
	}
	if !m.effects.lifetime.closeAndWait(limit) && len(m.effects.abandoned) == 0 {
		for _, lane := range []*effectLane{&m.effects.main, &m.effects.input} {
			if lane.active != nil {
				m.effects.abandoned = append(m.effects.abandoned, effectName(lane.active.request)+" was still running")
			}
		}
	}
}

// AbandonedEffects lists effects whose outcome the manager could not confirm
// before it exited.
func (m *Model) AbandonedEffects() []string { return slices.Clone(m.effects.abandoned) }

func (m *Model) enqueueEffect(request effectRequest, chain uint64, first bool) {
	if m.effects.quitting && !first {
		return
	}
	if m.effects.lifetime == nil {
		m.effects.lifetime = &effectLifetime{}
	}
	m.effects.nextID++
	id := m.effects.nextID
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
	work := m.captureEffect(request)
	lifetime := m.effects.lifetime
	var once sync.Once
	var completed effectCompletedMsg
	job := &effectJob{id: id, chain: chain, request: request}
	lane := m.laneFor(request)
	job.command = func() tea.Msg {
		once.Do(func() {
			completed.id = id
			if !lifetime.begin() {
				completed.err = errEffectsStopped
				return
			}
			defer lifetime.running.Done()
			completed.result, completed.err = work()
			completed.finishedAt = time.Now()
		})
		return completed
	}
	if first {
		lane.pending = append([]*effectJob{job}, lane.pending...)
	} else {
		lane.pending = append(lane.pending, job)
	}
}

func (m *Model) nextEffectCmd() tea.Cmd {
	var commands []tea.Cmd
	idle := true
	for _, lane := range []*effectLane{&m.effects.main, &m.effects.input} {
		if lane.active == nil && len(lane.pending) > 0 {
			lane.active, lane.pending = lane.pending[0], lane.pending[1:]
			commands = append(commands, lane.active.command)
		}
		if lane.active != nil {
			idle = false
		}
	}
	if idle && m.effects.quitting {
		return tea.Quit
	}
	return tea.Batch(commands...)
}

func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.launchHint.install != nil {
		m.reportErr("install may still be running in " + m.launchHint.install.name + "; finish it, kill its terminal with the session controls, or attach and interrupt it before quitting")
		return m, nil
	}
	starting := func(job *effectJob) (installStartRequest, bool) {
		if job == nil {
			return installStartRequest{}, false
		}
		request, ok := job.request.(installStartRequest)
		return request, ok
	}
	if request, ok := starting(m.effects.main.active); ok {
		m.reportErr("install for " + request.binary + " is still starting; wait for its installer shell before quitting")
		return m, nil
	}
	for _, job := range m.effects.main.pending {
		if request, ok := starting(job); ok {
			m.reportErr("install for " + request.binary + " is still queued; wait for its installer shell before quitting")
			return m, nil
		}
	}
	if m.effects.quitting {
		return m, m.abandonEffects()
	}
	m.prepareSplitForQuit()
	m.effects.quitting = true
	return m, m.nextEffectCmd()
}

// abandonEffects answers a second quit while the drain waits: it stops
// waiting and records what may or may not have happened, rather than leaving
// a stuck effect as the only way out being kill -9.
func (m *Model) abandonEffects() tea.Cmd {
	for _, lane := range []*effectLane{&m.effects.main, &m.effects.input} {
		if lane.active != nil {
			m.effects.abandoned = append(m.effects.abandoned, effectName(lane.active.request)+" was still running")
		}
		for _, job := range lane.pending {
			m.effects.abandoned = append(m.effects.abandoned, effectName(job.request)+" never started")
		}
		lane.pending = nil
	}
	return tea.Quit
}

func (m *Model) handleEffectCompleted(msg effectCompletedMsg) (tea.Model, tea.Cmd) {
	var job *effectJob
	for _, lane := range []*effectLane{&m.effects.main, &m.effects.input} {
		if lane.active != nil && lane.active.id == msg.id {
			job, lane.active = lane.active, nil
		}
	}
	if job == nil {
		return m, nil
	}
	// Keys, paste and mouse change no stored row, so they neither fence a
	// listing nor ask for a poll; a typed prompt records LastPrompt and does.
	if request, ok := job.request.(inputRequest); ok && request.kind != inputPrompt {
		result, _ := msg.result.(inputEffectResult)
		m.applyInputEffect(result, msg.err)
		return m, m.nextEffectCmd()
	}
	switch job.request.(type) {
	case inputRequest, quickSendRequest, installStartRequest, lifecycleRequest, afterTurnRequest, railRequest, forkRequest, spawnRequest, groupRequest, renameRequest, focusRequest, ackRequest, attachRequest:
		if msg.finishedAt.After(m.effects.latestObservation) {
			m.effects.latestObservation = msg.finishedAt
		}
	case reviewEffectRequest:
		// A send types into the agent pane, so a poll captured before it
		// is stale exactly like a lifecycle mutation; reads are not.
		if job.request.(reviewEffectRequest).op == reviewOpSend && msg.finishedAt.After(m.effects.latestObservation) {
			m.effects.latestObservation = msg.finishedAt
		}
	}
	var command tea.Cmd
	switch result := msg.result.(type) {
	case inputEffectResult:
		m.applyInputEffect(result, msg.err)
	case quickSendResult:
		command = m.applyQuickSend(job.request.(quickSendRequest), result, msg.err)
	case noticeDismissResult:
		m.applyNoticeDismiss(job, msg.err)
	case splitSaveResult:
		m.applySplitSave(job, msg.err)
	case installStartResult:
		command = m.applyInstallStart(job.request.(installStartRequest), result, msg.err)
	case installSettleResult:
		command = m.applyInstallSettle(job.request.(installSettleRequest), result, msg.err)
	case lifecycleEffectResult:
		command = m.applyLifecycleEffect(job.request.(lifecycleRequest), result, msg.err)
	case afterTurnResult:
		command = m.applyAfterTurn(job.request.(afterTurnRequest), result, msg.err)
	case railEffectResult:
		command = m.applyRailEffect(job, result, msg.err)
	case forkEffectResult:
		command = m.applyForkEffect(job.request.(forkRequest), result, msg.err)
	case geometryEffectResult:
		m.applyGeometryEffect(result, msg.err)
	case spawnEffectResult:
		command = m.applySpawnEffect(job.request.(spawnRequest), result, msg.err)
	case groupEffectResult:
		command = m.applyGroupEffect(job.request.(groupRequest), result, msg.err)
	case renameEffectResult:
		m.applyRenameEffect(job, result, msg.err)
	case moveDialogCloseResult:
		m.applyMoveDialogClose(job.request.(moveDialogClose))
	case settingsEffectResult:
		command = m.applySettingsEffect(job, result, msg.err)
	case reviewEffectResult:
		command = m.applyReviewEffect(job, result, msg.err)
	case keysEffectResult:
		command = m.applyKeysEffect(job, result, msg.err)
	case focusEffectResult:
		command = m.applyFocusEffect(job.request.(focusRequest), result, msg.err)
	case ackEffectResult:
		m.applyAckEffect(job, result, msg.err)
	case detachEffectResult:
		command = m.applyDetachEffect(job.request.(detachRequest), result, msg.err)
	case attachEffectResult:
		command = m.applyAttachEffect(job.request.(attachRequest), result, msg.err)
	default:
		if msg.err != nil {
			m.reportErr(msg.err.Error())
		}
	}
	// A settle check runs once per poll; asking for a poll after it would
	// spin the poller for as long as the installer runs.
	if _, settling := job.request.(installSettleRequest); m.poller != nil && !settling {
		m.requestRefresh()
	}
	return m, tea.Batch(command, m.nextEffectCmd())
}

func effectName(request effectRequest) string {
	switch request.(type) {
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
		return "session lifecycle change"
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
	case reviewEffectRequest:
		return "review change"
	case keysRequest:
		return "key binding save"
	case focusRequest, ackRequest, detachRequest, attachRequest:
		return "focus or attach"
	}
	return "effect"
}

func (m *Model) routeEffectMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case effectCompletedMsg:
		return routed(m.handleEffectCompleted(msg))
	}
	return nil, nil, false
}
