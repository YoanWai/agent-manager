package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) applyPromptsRead(dialogGen uint64, read uisettings.PromptsRead) {
	card, open := surfaceOf[*uisettings.Surface](m)
	if !open || dialogGen != m.settings.Gen {
		return
	}
	notice := card.State().LoadedPrompts(read)
	if notice.Changed {
		m.setNotice(notice)
	}
}

// runPromptsResult runs the catalog work a panel key asked for: reads go
// straight to the store off Update, writes go on the effect lane.
func (m *Model) runPromptsResult(d *settingsState, result uisettings.Result) tea.Cmd {
	gen, dialogGen := d.Prompts.Gen(), m.settings.Gen
	switch {
	case result.LoadPrompts:
		return savedPromptsLoadCmd(m.services.store, savedForSettings, gen, dialogGen)
	case result.SavePrompt != nil:
		m.enqueueEffect(savedPromptRequest{Gen: gen, DialogGen: dialogGen, Save: result.SavePrompt}, 0, false)
		return m.nextEffectCmd()
	case result.DeletePrompt != "":
		m.enqueueEffect(savedPromptRequest{Gen: gen, DialogGen: dialogGen, Remove: result.DeletePrompt}, 0, false)
		return m.nextEffectCmd()
	}
	return nil
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
	switch msg.Target {
	case savedForSettings:
		m.applyPromptsRead(msg.DialogGen, uisettings.PromptsRead{Gen: msg.Gen, Rows: settingsPrompts(msg.Rows), Err: msg.Err})
	case savedForForm:
		if f, ok := m.formFor(int(msg.Gen)); ok {
			f.SetSaved(spawnSaved(msg.Rows), msg.Err)
		}
	case savedForQuick:
		if q, ok := m.quickFor(int(msg.Gen)); ok {
			q.LoadSaved(spawnSaved(msg.Rows), msg.Err)
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
