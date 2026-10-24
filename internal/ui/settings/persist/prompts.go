package persist

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

type SavedTarget uint8

const (
	SavedForSettings SavedTarget = iota
	SavedForForm
	SavedForQuick
	SavedForFocus
)

// SavedMsg is the saved prompts catalog as one read found it. Gen is the
// asking surface's generation, DialogGen the Settings visit for the panel.
type SavedMsg struct {
	Target    SavedTarget
	Gen       uint64
	DialogGen uint64
	Rows      []store.SessionPreset
	Err       error
}

// SavedCmd reads the catalog off the update path.
func SavedCmd(presets Presets, target SavedTarget, gen, dialogGen uint64) tea.Cmd {
	return func() tea.Msg {
		rows, err := presets.SessionPresets()
		return SavedMsg{Target: target, Gen: gen, DialogGen: dialogGen, Rows: rows, Err: err}
	}
}

// PromptRequest writes the catalog from the Settings panel: a save, or the
// delete of one name.
type PromptRequest struct {
	Gen       uint64
	DialogGen uint64
	Save      *uisettings.PromptSave
	Remove    string
}

// PromptResult is the catalog read back once the write landed.
type PromptResult struct {
	Rows      []store.SessionPreset
	Committed bool
}

// RunPrompt saves or deletes one name, then reads the catalog back.
func RunPrompt(request PromptRequest, presets Presets) (PromptResult, error) {
	var err error
	if request.Save != nil {
		err = presets.SaveSessionPreset(request.Save.Previous, store.SessionPreset{Name: request.Save.Prompt.Name, Instructions: request.Save.Prompt.Text})
	} else {
		_, err = presets.DeleteSessionPreset(request.Remove)
	}
	if err != nil {
		return PromptResult{}, err
	}
	rows, err := presets.SessionPresets()
	return PromptResult{Rows: rows, Committed: true}, err
}

// Asked is the catalog work a panel key asked for: either a read, or the
// write its request names. A key that asks for neither returns false.
func Asked(result uisettings.Result, gen, dialogGen uint64) (PromptRequest, bool, bool) {
	request := PromptRequest{Gen: gen, DialogGen: dialogGen}
	switch {
	case result.LoadPrompts:
		return request, true, true
	case result.SavePrompt != nil:
		request.Save = result.SavePrompt
		return request, false, true
	case result.DeletePrompt != "":
		request.Remove = result.DeletePrompt
		return request, false, true
	}
	return PromptRequest{}, false, false
}
