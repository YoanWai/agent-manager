package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	tea "github.com/charmbracelet/bubbletea"
)

type inputRequest struct {
	request focusruntime.Request
}

func (inputRequest) EffectRequest() {}

func (inputRequest) effectName() string { return "input to a session" }

func (value inputRequest) copied() effectRequest {
	value.request = value.request.Copied()
	return value
}

func (request inputRequest) run(services effectServices) (uieffects.Result, error) {
	result, err := services.inputWriter().Run(request.request)
	return inputEffectResult{
		inputID:   result.InputID,
		candidate: typedPrompt(result.Candidate),
		promptID:  result.PromptID,
		prompt:    result.Prompt,
	}, err
}

type inputEffectResult struct {
	inputID   string
	candidate *typedPromptCandidate
	promptID  string
	prompt    string
}

func (inputEffectResult) EffectResult() {}

func (result inputEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applyInputEffect(result, err)
	return nil
}

// inputWriter is the store, tmux server and watcher one queued write runs
// against.
func (s effectServices) inputWriter() focusruntime.Writer {
	return focusruntime.Writer{
		Store:  s.store,
		Driver: s.driver,
		Engine: s.engine,
		Watch:  s.watch,
		Paste:  s.pasteInput,
	}
}

func (m *Model) dispatchInput(request focusruntime.Request) {
	request = request.Copied()
	if request.Kind != focusruntime.Prompt && !request.Counted {
		request.Counted = m.focus.runtime.CountWrite(request.Session.ID)
	}
	m.enqueueEffect(inputRequest{request: request}, 0, false)
}

func (m *Model) applyInputEffect(result inputEffectResult, err error) {
	if err != nil {
		m.reportErr(err.Error())
		return
	}
	if !m.effects.Quitting && result.inputID != "" && m.focused() && m.focus.runtime.Ready() {
		if selected, ok := m.selected(); ok && selected.ID == result.inputID {
			m.focus.runtime.Retry()
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

// typedPrompt carries a captured composer draft into the root's own
// candidate, which the startup tick judges a send or a menu enter.
func typedPrompt(candidate *focusruntime.Candidate) *typedPromptCandidate {
	if candidate == nil {
		return nil
	}
	return &typedPromptCandidate{session: candidate.Session, id: candidate.ID, text: candidate.Text, at: candidate.At}
}
