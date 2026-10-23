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
func (m *Model) runPromptsResult(d *settingsState, result uisettings.Result) tea.Cmd {
	gen, dialogGen := d.Prompts.Gen(), m.settings.gen
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
	card, open := surfaceOf[*uisettings.Surface](m)
	if !open || dialogGen != m.settings.gen {
		return
	}
	notice := card.State().LoadedPrompts(read)
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
		if f, ok := m.formFor(int(msg.gen)); ok {
			f.SetSaved(spawnSaved(msg.rows), msg.err)
		}
	case savedForQuick:
		if q, ok := m.quickFor(int(msg.gen)); ok {
			q.LoadSaved(spawnSaved(msg.rows), msg.err)
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
