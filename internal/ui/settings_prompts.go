package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// savedPromptsTarget is the surface a catalog read was for.
type savedPromptsTarget uint8

const (
	savedForSettings savedPromptsTarget = iota
	savedForForm
	savedForQuick
	savedForFocus
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

func (savedPromptRequest) effectRequest() {}

// savedPromptResult is the catalog read back once the write landed.
type savedPromptResult struct {
	rows      []store.SessionPreset
	committed bool
}

func (savedPromptResult) effectResult() {}

func (s effectServices) runSavedPrompt(request savedPromptRequest) (effectResult, error) {
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

func (m *Model) applySavedPromptEffect(request savedPromptRequest, result savedPromptResult, err error) {
	selected := ""
	if request.save != nil {
		selected = request.save.SelectedName()
	}
	m.applyPromptsRead(request.dialogGen, uisettings.PromptsRead{
		Gen: request.gen, Rows: settingsPrompts(result.rows), Err: err,
		Mutation: true, Committed: result.committed, Selected: selected,
	})
}

// runPromptsResult runs the catalog work a panel key asked for: reads go
// straight to the store off Update, writes go on the effect lane.
func (m *Model) runPromptsResult(result uisettings.Result) tea.Cmd {
	gen, dialogGen := m.settings.dialog.Prompts.Gen(), m.settings.gen
	switch {
	case result.LoadPrompts:
		return savedPromptsLoadCmd(m.services.store, savedForSettings, gen, dialogGen)
	case result.SavePrompt != nil:
		m.enqueueEffect(savedPromptRequest{gen: gen, dialogGen: dialogGen, save: result.SavePrompt}, 0, false)
		return m.nextEffectCmd()
	case result.DeletePrompt != "":
		m.enqueueEffect(savedPromptRequest{gen: gen, dialogGen: dialogGen, remove: result.DeletePrompt}, 0, false)
		return m.nextEffectCmd()
	}
	return nil
}

func (m *Model) applyPromptsRead(dialogGen uint64, read uisettings.PromptsRead) {
	if m.mode != modeSettings || dialogGen != m.settings.gen {
		return
	}
	notice := m.settings.dialog.LoadedPrompts(read)
	if notice.Changed {
		m.setNotice(notice)
	}
}

func (m *Model) setNotice(notice uisettings.Notice) {
	if notice.Text == "" {
		m.clearErr()
		return
	}
	m.reportErr(notice.Text)
}

// handleSavedPromptsLoaded hands a catalog read to the surface that asked,
// while it is still the one that asked.
func (m *Model) handleSavedPromptsLoaded(msg savedPromptsLoadedMsg) {
	switch msg.target {
	case savedForSettings:
		m.applyPromptsRead(msg.dialogGen, uisettings.PromptsRead{Gen: msg.gen, Rows: settingsPrompts(msg.rows), Err: msg.err})
	case savedForForm:
		if m.mode == modeForm && uint64(m.form.prompt.gen) == msg.gen {
			m.form.SetSaved(spawnSaved(msg.rows), msg.err)
		}
	case savedForQuick:
		if m.quick.active && uint64(m.quick.gen) == msg.gen {
			m.quick.LoadSaved(spawnSaved(msg.rows), msg.err)
		}
	case savedForFocus:
		m.loadFocusSaved(msg)
	}
}

func spawnSaved(rows []store.SessionPreset) []spawn.Saved {
	saved := make([]spawn.Saved, len(rows))
	for i, row := range rows {
		saved[i] = spawn.Saved{Name: row.Name, Text: row.Instructions}
	}
	return saved
}

func settingsPrompts(rows []store.SessionPreset) []uisettings.Prompt {
	prompts := make([]uisettings.Prompt, len(rows))
	for i, row := range rows {
		prompts[i] = uisettings.Prompt{Name: row.Name, Text: row.Instructions}
	}
	return prompts
}
