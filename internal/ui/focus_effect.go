package ui

import (
	"errors"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

type focusRequest struct {
	generation uint64
	sessionID  string
}

func (focusRequest) effectRequest() {}

type focusEffectResult struct {
	fatal error
}

func (focusEffectResult) effectResult() {}

func (s effectServices) runFocus(request focusRequest) (effectResult, error) {
	result := focusEffectResult{}
	if !s.driver.Exists(request.sessionID) {
		return result, errors.New(deadSessionHint)
	}
	sess, err := s.store.Get(request.sessionID)
	if err != nil {
		return result, err
	}
	if sess.Status == status.Finished {
		if ackErr := s.store.AcknowledgeFinished(request.sessionID); ackErr != nil {
			return result, ackErr
		}
	}
	return result, nil
}

func (m *Model) applyFocusEffect(request focusRequest, result focusEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if m.effects.quitting || request.generation != m.gens.foreground {
		return nil
	}
	if m.mode != modeList {
		return nil
	}
	sess, ok := m.selected()
	if !ok || sess.ID != request.sessionID {
		return nil
	}
	return m.enterFocus(sess)
}

func (m *Model) enterFocus(sess store.Session) tea.Cmd {
	if m.quick.active {
		m.quick.active = false
		m.quick.release()
	}
	m.mode = modeFocus
	if m.prefs.fullLayout {
		m.pinFullFocusPane(sess.ID)
	}
	if m.focus.runtime.watch != nil {
		m.focus.runtime.watch.retryNow()
	}
	m.watchSelection()
	pane := m.focus.pane.Pane()
	keepPaneFacts := m.focus.runtime.watch != nil &&
		m.focus.runtime.watch.serving(sess.ID) && pane.SessionID == sess.ID
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: sess.ID, KeepPaneFacts: keepPaneFacts})
	return tea.Batch(tea.EnableMouseCellMotion, m.cursorBlink())
}

type ackRequest struct {
	sessionID string
}

func (ackRequest) effectRequest() {}

type ackEffectResult struct{}

func (ackEffectResult) effectResult() {}

func (s effectServices) runAck(request ackRequest) (effectResult, error) {
	if err := s.store.AcknowledgeFinished(request.sessionID); err != nil {
		return ackEffectResult{}, err
	}
	return ackEffectResult{}, nil
}

func (m *Model) applyAckEffect(job *effectJob, result ackEffectResult, err error) tea.Cmd {
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

func (detachRequest) effectRequest() {}

type detachEffectResult struct {
	request  string
	clearErr error
}

func (detachEffectResult) effectResult() {}

func (s effectServices) runDetach(request detachRequest) (effectResult, error) {
	result := detachEffectResult{}
	marker, err := s.driver.PendingRequest()
	if err != nil {
		return result, err
	}
	if marker == "" {
		return result, nil
	}
	result.request = marker
	result.clearErr = s.driver.ClearRequest()
	return result, nil
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
	if m.effects.quitting {
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
		if m.mode == modeDiff {
			m.reviewNav.ret = reviewReturn{kind: reviewReturnAttach, sessionID: sess.ID}
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
	if m.mode != modeList || m.rail.InputMode() != uirail.BrowseMode || m.quick.active || m.layout.split.resizeMode || m.layout.split.dragging {
		return true
	}
	for _, job := range m.effects.main.pending {
		switch job.request.(type) {
		case focusRequest, attachRequest:
			return true
		}
	}
	return false
}
