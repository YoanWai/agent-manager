package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	tea "github.com/charmbracelet/bubbletea"
)

// keysRequest is the key picker's tables at dispatch.
type keysRequest uipersist.KeysRequest

func (request keysRequest) EffectRequest() {}

func (request keysRequest) copied() effectRequest { return request }

func (keysRequest) effectName() string { return "key binding save" }

func (request keysRequest) saved() uipersist.KeysRequest { return uipersist.KeysRequest(request) }

func (request keysRequest) run(services effectServices) (uieffects.Result, error) {
	outcome, err := uipersist.RunKeys(request.saved(), services.keySaver())
	return keysEffectResult(outcome), err
}

// keysEffectResult reports which tables landed.
type keysEffectResult uipersist.KeysResult

func (keysEffectResult) EffectResult() {}

func (result keysEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return uipersist.ApplyKeys(settingsKeysEnv{m}, job.Request.(keysRequest).saved(), uipersist.KeysResult(result), err)
}

// settingsKeysEnv is the root as a key save completion reaches it.
type settingsKeysEnv struct{ *Model }

func (e settingsKeysEnv) ReportErr(text string) { e.reportErr(text) }

func (e settingsKeysEnv) SaveKeys(list, session keybind.Table, listChanged, sessionChanged bool) tea.Cmd {
	if listChanged {
		e.services.listKeys = list
	}
	if sessionChanged {
		e.services.keys = session
		e.services.tmux.SetSessionKeys(session)
		return e.refreshExistingSessionUX
	}
	return nil
}
