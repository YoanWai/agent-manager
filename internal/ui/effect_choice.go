package ui

import (
	"fmt"
	"maps"
	"slices"
)

// choiceSaveRequest persists a CLI's kept choice or recent models. The
// settings cache already holds the value, so the next form reads it
// without waiting for the write.
type choiceSaveRequest struct {
	key   string
	value string
}

func (choiceSaveRequest) effectRequest() {}

type choiceSaveResult struct{}

func (choiceSaveResult) effectResult() {}

func (s effectServices) runChoiceSave(request choiceSaveRequest) (effectResult, error) {
	if err := s.store.SetSetting(request.key, request.value); err != nil {
		return choiceSaveResult{}, fmt.Errorf("saving %s: %w", request.key, err)
	}
	return choiceSaveResult{}, nil
}

func (m *Model) saveChoiceSetting(key, value string) {
	m.settings.cache.applyValues([]settingValue{{key: key, value: value}})
	m.enqueueEffect(choiceSaveRequest{key: key, value: value}, 0, false)
}

func (m *Model) choiceSettingKeys() []string {
	return choiceSettingKeys(slices.Sorted(maps.Keys(m.services.cfg.Tools)))
}

// refreshChoicePrefs takes a newer read of the CLI's kept choice into an
// open choice.
func (m *Model) refreshChoicePrefs(toolName string, ch *choice) { ch.refreshPrefs(m, toolName) }

// refreshPrefs takes a newer read of the CLI's kept choice while the open
// choice still waits to place it.
func (ch *choice) refreshPrefs(h choiceHost, toolName string) {
	ch.recent = recentModels(h, toolName)
	if ch.saved != nil {
		ch.saved = savedChoice(h, toolName)
		ch.restore(h, toolName)
	}
}

// choiceSettingKeys are the per-CLI keys the form and quick prompt read
// from the settings cache.
func choiceSettingKeys(tools []string) []string {
	keys := make([]string, 0, 2*len(tools))
	for _, name := range tools {
		keys = append(keys, savedChoiceKey(name), recentModelsKey(name))
	}
	return keys
}
