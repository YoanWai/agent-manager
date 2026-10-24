package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	tea "github.com/charmbracelet/bubbletea"
)

// focusProber is the store and tmux server the entry and exit probes read.
func (s effectServices) focusProber() focusruntime.Prober {
	return focusruntime.Prober{Store: s.store, Driver: s.driver}
}

type focusRequest struct {
	generation uint64
	sessionID  string
}

func (focusRequest) EffectRequest() {}

func (request focusRequest) copied() effectRequest { return request }

func (focusRequest) effectName() string { return "focus or attach" }

func (request focusRequest) run(services effectServices) (uieffects.Result, error) {
	return focusEffectResult{}, services.focusProber().Probe(request.sessionID)
}

type focusEffectResult struct{}

func (focusEffectResult) EffectResult() {}

func (result focusEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyFocusEffect(job.Request.(focusRequest), err)
}

func (m *Model) applyFocusEffect(request focusRequest, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if m.effects.Quitting || request.generation != m.gens.foreground {
		return nil
	}
	if !m.listClear() {
		return nil
	}
	sess, ok := m.selected()
	if !ok || sess.ID != request.sessionID {
		return nil
	}
	return m.enterFocus(sess)
}

func (m *Model) enterFocus(sess store.Session) tea.Cmd {
	m.clearSurfaces()
	m.push(newFocusSurface(m))
	if m.prefs.fullLayout {
		m.layout.panes.PinFullFocus(paneSizing{m}, sess.ID, m.layout.width, m.listBodyHeight())
	}
	m.focus.runtime.Retry()
	m.watchSelection()
	keepPaneFacts := m.focus.runtime.KeepsPaneFacts(&m.focus.pane, sess.ID)
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: sess.ID, KeepPaneFacts: keepPaneFacts})
	return tea.Batch(tea.EnableMouseCellMotion, m.cursorBlink())
}

type ackRequest struct {
	sessionID string
}

func (ackRequest) EffectRequest() {}

func (request ackRequest) copied() effectRequest { return request }

func (ackRequest) effectName() string { return "focus or attach" }

func (request ackRequest) run(services effectServices) (uieffects.Result, error) {
	return ackEffectResult{}, services.focusProber().Ack(request.sessionID)
}

type ackEffectResult struct{}

func (ackEffectResult) EffectResult() {}

func (result ackEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applyAckEffect(result, err)
	return nil
}

func (m *Model) applyAckEffect(_ ackEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	m.requestRefresh()
	return nil
}

type detachRequest struct {
	sessionID string
}

func (detachRequest) EffectRequest() {}

func (request detachRequest) copied() effectRequest { return request }

func (detachRequest) effectName() string { return "focus or attach" }

func (request detachRequest) run(services effectServices) (uieffects.Result, error) {
	detached, err := services.focusProber().Detached(request.sessionID)
	return detachEffectResult{request: detached.Request, clearErr: detached.ClearErr}, err
}

type detachEffectResult struct {
	request  string
	clearErr error
}

func (detachEffectResult) EffectResult() {}

func (result detachEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyDetachEffect(job.Request.(detachRequest), result, err)
}

func (m *Model) applyDetachEffect(request detachRequest, result detachEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if result.clearErr != nil {
		m.reportErr(result.clearErr.Error())
		m.requestRefresh()
		return nil
	}
	if m.effects.Quitting {
		return nil
	}
	if result.request == "" {
		m.requestRefresh()
		return nil
	}
	// The marker is already cleared, so a request the user moved past
	// stays on the status bar instead of vanishing; moving the cursor is
	// not moving past it.
	if m.foregroundTakenSinceDetach() {
		name := request.sessionID
		if sess, ok := m.sessionByID(request.sessionID); ok {
			name = sess.Name
		}
		m.reportWarn(fmt.Sprintf("the %s asked for in %s did not open: something else took the screen first; ask again", result.request, name))
		m.requestRefresh()
		return nil
	}
	m.focusSession(request.sessionID)
	sess, ok := m.selected()
	if !ok || sess.ID != request.sessionID {
		m.reportErr("the session that asked for it has left the list")
		m.requestRefresh()
		return nil
	}
	switch result.request {
	case tmux.RequestReview:
		cmd := m.openDiff()
		if m.base == baseReview {
			m.reviewNav.Set(reviewReturn{Kind: reviewReturnAttach, SessionID: sess.ID})
		}
		return cmd
	case tmux.RequestEditor:
		_, cmd := m.openEditorWithReaderForReturn(systemTerminalDirectoryReader{
			tmux: m.services.tmux,
			dirs: systemDirectoryPreflight{git: m.services.gitDrv},
		}, sess.ID)
		return cmd
	}
	m.requestRefresh()
	return nil
}

// foregroundTakenSinceDetach reports whether, since the detach, the user
// opened a view, a rail prompt or a resize, or asked to enter a session.
func (m *Model) foregroundTakenSinceDetach() bool {
	if m.base != baseList || len(m.surfaces) > 0 || m.rail.Busy() {
		return true
	}
	for _, job := range m.effects.Main.Pending {
		switch job.Request.(type) {
		case focusRequest, attachRequest:
			return true
		}
	}
	return false
}
