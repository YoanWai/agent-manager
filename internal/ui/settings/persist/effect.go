package persist

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

// Services is what the Settings effect lane captures for one job: the writer
// a save writes and reads back through, the key saver a key save writes
// through, and the preset catalog a prompt change reads back.
type Services struct {
	Writer  Writer
	Keys    KeySaver
	Presets Presets
}

func (SaveResult) EffectResult() {}

func (KeysResult) EffectResult() {}

func (PromptResult) EffectResult() {}

func (r SaveRequest) EffectName() string { return "settings save" }

func (r SaveRequest) EffectCopy() uieffects.Request { return r.Clone() }

// Run writes the captured values in the order the dialog listed them.
func (r SaveRequest) Run(services Services) (uieffects.Result, error) {
	outcome, err := RunSave(r.Clone(), services.Writer)
	return outcome, err
}

func (r KeysRequest) EffectName() string { return "key binding save" }

func (r KeysRequest) EffectCopy() uieffects.Request { return r }

// Run writes the changed key tables in list-then-session order.
func (r KeysRequest) Run(services Services) (uieffects.Result, error) {
	return RunKeys(r, services.Keys)
}

func (r PromptRequest) EffectName() string { return "saved prompt change" }

func (r PromptRequest) EffectCopy() uieffects.Request { return r }

// Run saves or deletes one name, then reads the catalog back.
func (r PromptRequest) Run(services Services) (uieffects.Result, error) {
	return RunPrompt(r, services.Presets)
}

// KeysToSubmit is the key save the picker's tables ask for, if they ask for
// one at all.
func KeysToSubmit(change uisettings.KeysChange, changed bool) (KeysRequest, bool) {
	if !changed {
		return KeysRequest{}, false
	}
	return KeysRequest{
		List:           change.List,
		Session:        change.Session,
		ListChanged:    change.ListChanged,
		SessionChanged: change.SessionChanged,
	}, true
}
