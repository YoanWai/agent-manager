package ui

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

type inputKind uint8

const (
	inputKeys inputKind = iota
	inputMouse
	inputPaste
	inputPrompt
)

type inputRequest struct {
	kind    inputKind
	session store.Session
	command string
	args    []string
	text    string
	submit  bool
}

func (inputRequest) effectRequest() {}

type inputEffectResult struct {
	inputID   string
	candidate *typedPromptCandidate
	promptID  string
	prompt    string
}

func (inputEffectResult) effectResult() {}

func (m *Model) dispatchInput(request inputRequest) {
	request.args = slices.Clone(request.args)
	m.enqueueEffect(request, 0, false)
}

func (s effectServices) runInput(request inputRequest) (effectResult, error) {
	result := inputEffectResult{}
	current, err := s.store.Get(request.session.ID)
	if err != nil {
		return result, err
	}
	if !current.CreatedAt.Equal(request.session.CreatedAt) || current.TmuxSocket != request.session.TmuxSocket || !current.LaunchTime().Equal(request.session.LaunchTime()) {
		return result, errors.New("input target changed its creation, launch, or socket before the accepted input ran")
	}
	if request.kind == inputPrompt {
		result.promptID, result.prompt = current.ID, request.text
		return result, s.store.SetLastPromptForLaunch(request.session, result.prompt)
	}
	if request.submit && s.engine != nil {
		if pane, captureErr := s.driver.CapturePane(current.ID); captureErr == nil {
			if draft, ok := s.engine.InputDraft(current.Tool, ansi.Strip(pane)); ok {
				result.candidate = &typedPromptCandidate{id: current.ID, session: current, text: draft, at: time.Now()}
			}
		}
	}
	result.inputID = current.ID
	switch request.kind {
	case inputKeys, inputMouse:
		available := false
		if s.watch != nil && request.command != "" {
			available, err = s.watch.forward(request.command)
			if available && err != nil {
				err = fmt.Errorf("focused input may have reached the pane and was not replayed: %w", err)
			}
		}
		if !available {
			if request.kind == inputKeys {
				err = s.driver.SendRaw(request.command)
			} else {
				err = s.driver.SendCommand(request.args...)
			}
		}
	case inputPaste:
		err = s.pasteInput(s.driver, current.ID, request.text)
	}
	return result, err
}

func (m *Model) applyInputEffect(result inputEffectResult, err error) {
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if !m.effects.quitting && result.inputID != "" && m.mode == modeFocus && m.focusRuntime.watch != nil {
		if selected, ok := m.selected(); ok && selected.ID == result.inputID {
			m.focusRuntime.watch.retryNow()
			m.watchSelection()
		}
	}
	if result.candidate != nil {
		m.startup.pendingTyped = result.candidate
	}
	if result.promptID != "" {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == result.promptID {
				m.workspace.sessions[i].LastPrompt = result.prompt
			}
		}
	}
}
