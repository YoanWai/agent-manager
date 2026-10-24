package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	tea "github.com/charmbracelet/bubbletea"
)

type (
	settingsLoadRequest = uipersist.LoadRequest
	settingsLoadResult  = uipersist.LoadResult
	settingsLoadedMsg   = uipersist.LoadedMsg
	settingsValueReader = uipersist.Reader
	storeSettingWriter  = uipersist.StoreWriter
)

const (
	settingsLoadDialog = uipersist.DialogTarget
	settingsLoadForm   = uipersist.FormTarget
	settingsLoadQuick  = uipersist.QuickTarget
)

// settingsLoadCmd reads the store off the update path for the surface that
// asked.
func settingsLoadCmd(request settingsLoadRequest, reader settingsValueReader) tea.Cmd {
	return uipersist.LoadCmd(request, reader)
}

// settingsEnv is the root as a save completion reaches it.
type settingsEnv struct{ *Model }

func (e settingsEnv) ReportErr(text string) { e.reportErr(text) }

func (e settingsEnv) Caveat(text string) { e.reportErr(e.errBar.text + "; " + text) }

func (e settingsEnv) MirrorPrefs(values []uisettings.Readback) string {
	return e.reconcileSettingsPrefs(values)
}

func (e settingsEnv) Restore(values []uisettings.Readback, hiddenRaw string, hiddenErr error, editors *uisettings.EditorsProbe) {
	if card, open := surfaceOf[*uisettings.Surface](e.Model); open {
		card.State().Restore(values, hiddenRaw, hiddenErr, editors)
	}
}

func (e settingsEnv) Update(staged bool) tea.Cmd {
	if !staged {
		e.release.Applying = false
		return nil
	}
	return e.release.Apply()
}

// settingsRequest captures the dialog's chosen preferences and CLI
// visibility at dispatch; the worker persists them outside Update in the
// order the dialog used to write them.
type settingsRequest struct {
	values       []settingValue
	hidden       []string
	followUpdate bool
	generation   uint64
}

func (settingsRequest) EffectRequest() {}

func (settingsRequest) effectName() string { return "settings save" }

func (value settingsRequest) copied() effectRequest {
	copied := value.saved().Clone()
	return settingsRequest{values: copied.Values, hidden: copied.Hidden, followUpdate: value.followUpdate, generation: value.generation}
}

func (request settingsRequest) saved() uipersist.SaveRequest {
	return uipersist.SaveRequest{
		Values:       request.values,
		Hidden:       request.hidden,
		StagedUpdate: request.followUpdate,
		Generation:   request.generation,
	}
}

func (request settingsRequest) run(services effectServices) (uieffects.Result, error) {
	return request.execute(services.settingWriter())
}

// execute writes the captured values through writer and reports what landed.
func (request settingsRequest) execute(writer uipersist.Writer) (settingsEffectResult, error) {
	outcome, err := uipersist.RunSave(request.saved(), writer)
	return settingsEffectResult{outcome: outcome}, err
}

// settingsEffectResult is what the worker durably committed, as the persist
// package reports it.
type settingsEffectResult struct{ outcome uipersist.SaveResult }

func (settingsEffectResult) EffectResult() {}

func (result settingsEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applySettingsEffect(job, result, err)
}

func (s effectServices) settingWriter() uipersist.Writer {
	return uipersist.StoreWriter{Store: s.store}
}

// keySaver writes one key table through the instance writer the job
// captured, or the store when the model was built without one.
func (s effectServices) keySaver() uipersist.KeySaver {
	if s.saveKeys != nil {
		return s.saveKeys
	}
	return s.store.SetKeys
}

// applySettingsEffect reconciles the worker's durable outcome, which the
// persist package decides: a partial failure's committed read-back reaches
// the prefs and the card that is still the one that asked.
func (m *Model) applySettingsEffect(job *uieffects.Job, result settingsEffectResult, err error) tea.Cmd {
	request := job.Request.(settingsRequest)
	return m.settings.ApplySave(settingsEnv{m}, request.saved(), result.outcome, err)
}

// reconcileSettingsPrefs restores the live-session prefs to the committed
// read-back; keys whose read failed keep their current value.
func (m *Model) reconcileSettingsPrefs(restored []uisettings.Readback) string {
	failed := 0
	for _, value := range restored {
		if value.Err != nil {
			failed++
			continue
		}
		m.mirrorPreference(value.Key, value.Value)
	}
	return uipersist.ReadFailureNote(failed)
}
