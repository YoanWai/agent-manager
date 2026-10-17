package ui

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
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
	// echo asks the focus watcher for a prompt capture once the input lands,
	// so the pane's echo of it shows without waiting out the frame budget.
	echo bool
	// counted marks a write the polled focus watcher counted, so it must
	// hear the write land whatever its outcome.
	counted bool
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
	if watch := m.focus.runtime.watch; watch != nil && request.kind != inputPrompt {
		request.counted = watch.CountWrite(request.session.ID)
	}
	m.enqueueEffect(request, 0, false)
}

func (s effectServices) runInput(request inputRequest) (effectResult, error) {
	if request.counted {
		defer s.watch.WriteLanded(request.session.ID)
	}
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
			blocks := 1
			if request.kind == inputMouse {
				// The guarded report answers with one block for if-shell and
				// one for whichever branch it ran.
				blocks = tmux.GuardedMouseReplies
			}
			available, err = s.watch.ForwardBlocks(request.command, blocks)
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
	if request.echo && err == nil && s.watch != nil {
		s.watch.InputSent(current.ID)
	}
	return result, err
}

func (m *Model) applyInputEffect(result inputEffectResult, err error) {
	if err != nil {
		m.reportErr(err.Error())
		return
	}
	if !m.effects.quitting && result.inputID != "" && m.mode == modeFocus && m.focus.runtime.watch != nil {
		if selected, ok := m.selected(); ok && selected.ID == result.inputID {
			m.focus.runtime.watch.RetryNow()
			m.watchSelection()
		}
	}
	if result.candidate != nil {
		m.holdTypedPrompt(*result.candidate)
	}
	if result.promptID != "" {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == result.promptID {
				m.workspace.sessions[i].LastPrompt = result.prompt
			}
		}
	}
}
