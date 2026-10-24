package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uicapture "github.com/YoanWai/agent-manager/internal/ui/preview/capture"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) applyFocusEffect(request focusruntime.ProbeRequest, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if m.effects.Quitting || request.Generation != m.gens.foreground {
		return nil
	}
	if !m.listClear() {
		return nil
	}
	sess, ok := m.selected()
	if !ok || sess.ID != request.SessionID {
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

func (m *Model) applyAckEffect(err error) {
	if err != nil {
		m.reportErr(err.Error())
		return
	}
	m.requestRefresh()
}

func (m *Model) applyDetachEffect(request focusruntime.DetachRequest, result focusruntime.DetachResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if result.ClearErr != nil {
		m.reportErr(result.ClearErr.Error())
		m.requestRefresh()
		return nil
	}
	if m.effects.Quitting {
		return nil
	}
	if result.Request == "" {
		m.requestRefresh()
		return nil
	}
	// The marker is already cleared, so a request the user moved past
	// stays on the status bar instead of vanishing; moving the cursor is
	// not moving past it.
	if m.foregroundTakenSinceDetach() {
		name := request.SessionID
		if sess, ok := m.sessionByID(request.SessionID); ok {
			name = sess.Name
		}
		m.reportWarn(fmt.Sprintf("the %s asked for in %s did not open: something else took the screen first; ask again", result.Request, name))
		m.requestRefresh()
		return nil
	}
	m.focusSession(request.SessionID)
	sess, ok := m.selected()
	if !ok || sess.ID != request.SessionID {
		m.reportErr("the session that asked for it has left the list")
		m.requestRefresh()
		return nil
	}
	switch result.Request {
	case tmux.RequestReview:
		cmd := m.openDiff()
		if m.base == baseReview {
			m.reviewNav.Set(uireviewsource.Return{Kind: uireviewsource.ReturnAttach, SessionID: sess.ID})
		}
		return cmd
	case tmux.RequestEditor:
		_, cmd := m.openEditorWithReaderForReturn(newSystemTerminalDirectoryReader(m), sess.ID)
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
		case focusruntime.ProbeRequest, uilifecycle.AttachRequest:
			return true
		}
	}
	return false
}

// dispatchInput queues one write to a focused session's pane.
func (m *Model) dispatchInput(request focusruntime.Request) {
	request = request.Copied()
	if request.Kind != focusruntime.Prompt && !request.Counted {
		request.Counted = m.focus.runtime.CountWrite(request.Session.ID)
	}
	m.enqueueEffect(focusruntime.InputRequest{Value: request}, 0, false)
}

func (m *Model) applyInputEffect(result focusruntime.InputResult, err error) {
	if err != nil {
		m.reportErr(err.Error())
		return
	}
	if !m.effects.Quitting && result.InputID != "" && m.focused() && m.focus.runtime.Ready() {
		if selected, ok := m.selected(); ok && selected.ID == result.InputID {
			m.focus.runtime.Retry()
			m.watchSelection()
		}
	}
	if result.Candidate != nil {
		m.holdTypedPrompt(candidatePrompt(*result.Candidate))
	}
	if result.PromptID != "" {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == result.PromptID {
				m.workspace.sessions[i].LastPrompt = result.Prompt
			}
		}
	}
}

// candidatePrompt carries a captured composer draft into the preview's held
// candidates, which the startup tick judges a send or a menu enter.
func candidatePrompt(candidate focusruntime.Candidate) uicapture.TypedPrompt {
	return uicapture.TypedPrompt{Session: candidate.Session, ID: candidate.ID, Text: candidate.Text, At: candidate.At}
}
