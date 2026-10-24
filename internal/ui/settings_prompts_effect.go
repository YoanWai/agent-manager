package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	tea "github.com/charmbracelet/bubbletea"
)

type (
	savedPromptsLoadedMsg = uipersist.SavedMsg
	savedPromptsTarget    = uipersist.SavedTarget
)

// savedPromptRequest writes the catalog from the Settings panel.
type savedPromptRequest uipersist.PromptRequest

// savedPromptResult is the catalog read back once the write landed.
type savedPromptResult uipersist.PromptResult

const (
	savedForSettings = uipersist.SavedForSettings
	savedForForm     = uipersist.SavedForForm
	savedForQuick    = uipersist.SavedForQuick
	savedForFocus    = uipersist.SavedForFocus
)

func (request savedPromptRequest) EffectRequest() {}

func (request savedPromptRequest) copied() effectRequest { return request }

func (savedPromptRequest) effectName() string { return "saved prompt change" }

func (request savedPromptRequest) run(services effectServices) (uieffects.Result, error) {
	outcome, err := uipersist.RunPrompt(uipersist.PromptRequest(request), services.store)
	return savedPromptResult(outcome), err
}

func (result savedPromptResult) EffectResult() {}

func (result savedPromptResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	request := uipersist.PromptRequest(job.Request.(savedPromptRequest))
	selected := ""
	if request.Save != nil {
		selected = request.Save.SelectedName()
	}
	m.applyPromptsRead(request.DialogGen, uisettings.PromptsRead{
		Gen: request.Gen, Rows: settingsPrompts(uipersist.PromptResult(result).Rows), Err: err,
		Mutation: true, Committed: uipersist.PromptResult(result).Committed, Selected: selected,
	})
	return nil
}

// savedPromptsLoadCmd reads the saved prompts catalog for the surface that
// acts on it.
func savedPromptsLoadCmd(presets uipersist.Presets, target savedPromptsTarget, gen, dialogGen uint64) tea.Cmd {
	return uipersist.SavedCmd(presets, target, gen, dialogGen)
}
