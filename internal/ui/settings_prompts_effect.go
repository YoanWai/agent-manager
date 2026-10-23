package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// savedPromptsLoadedMsg is the saved prompts catalog as one read found it.
// gen is the asking surface's generation, and dialogGen the Settings visit
// for the panel.
type savedPromptsLoadedMsg struct {
	target    savedPromptsTarget
	gen       uint64
	dialogGen uint64
	rows      []store.SessionPreset
	err       error
}

func savedPromptsLoadCmd(st *store.Store, target savedPromptsTarget, gen, dialogGen uint64) tea.Cmd {
	return func() tea.Msg {
		rows, err := st.SessionPresets()
		return savedPromptsLoadedMsg{target: target, gen: gen, dialogGen: dialogGen, rows: rows, err: err}
	}
}

// savedPromptRequest writes the catalog from the Settings panel: a save,
// or the delete of one name.
type savedPromptRequest struct {
	gen       uint64
	dialogGen uint64
	save      *uisettings.PromptSave
	remove    string
}

func (savedPromptRequest) EffectRequest() {}

func (request savedPromptRequest) copied() effectRequest { return request }

func (savedPromptRequest) effectName() string { return "saved prompt change" }

func (request savedPromptRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runSavedPrompt(request)
}

// savedPromptResult is the catalog read back once the write landed.
type savedPromptResult struct {
	rows      []store.SessionPreset
	committed bool
}

func (savedPromptResult) EffectResult() {}

func (result savedPromptResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applySavedPromptEffect(job.Request.(savedPromptRequest), result, err)
	return nil
}

func (s effectServices) runSavedPrompt(request savedPromptRequest) (uieffects.Result, error) {
	var err error
	if request.save != nil {
		err = s.store.SaveSessionPreset(request.save.Previous, store.SessionPreset{Name: request.save.Prompt.Name, Instructions: request.save.Prompt.Text})
	} else {
		_, err = s.store.DeleteSessionPreset(request.remove)
	}
	if err != nil {
		return savedPromptResult{}, err
	}
	rows, err := s.store.SessionPresets()
	return savedPromptResult{rows: rows, committed: true}, err
}
