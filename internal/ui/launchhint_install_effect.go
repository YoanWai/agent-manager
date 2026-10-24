package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilaunchhint "github.com/YoanWai/agent-manager/internal/ui/launchhint"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	tea "github.com/charmbracelet/bubbletea"
)

// installStartRequest freezes the setup dialog, shell selection, placement,
// retry and image ownership before any filesystem, store or tmux work begins.
type installStartRequest struct {
	request uiinstall.StartRequest
}

func (installStartRequest) EffectRequest() {}

func (installStartRequest) effectName() string { return "CLI install" }

func (value installStartRequest) copied() effectRequest {
	value.request = value.request.Copied()
	return value
}

func (request installStartRequest) run(services effectServices) (uieffects.Result, error) {
	result, err := uiinstall.RunStart(services.install(), request.request)
	return installStartResult{session: result.Session, install: result.Pending}, err
}

// installStartResult carries the committed shell row even when a later label
// or send step failed.
type installStartResult struct {
	session store.Session
	install *uiinstall.Pending
}

func (installStartResult) EffectResult() {}

func (result installStartResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	request := job.Request.(installStartRequest)
	return m.ledger.install.Started(installRoot{m}, request.request, uiinstall.StartResult{Session: result.session, Pending: result.install}, err)
}

type installSettleRequest struct {
	install uiinstall.SettleRequest
}

func (installSettleRequest) EffectRequest() {}

func (installSettleRequest) effectName() string { return "CLI install" }

func (value installSettleRequest) copied() effectRequest {
	value.install = value.install.Copied()
	return value
}

func (request installSettleRequest) run(services effectServices) (uieffects.Result, error) {
	result, err := uiinstall.RunSettle(services.install(), request.install)
	return installSettleResult{sessionID: result.SessionID, state: result.State, code: result.Code}, err
}

type installSettleResult struct {
	sessionID string
	state     uilaunchhint.SettleState
	code      string
}

func (installSettleResult) EffectResult() {}

func (result installSettleResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	request := job.Request.(installSettleRequest)
	return m.ledger.install.Settled(installRoot{m}, request.install, uiinstall.SettleResult{SessionID: result.sessionID, State: result.state, Code: result.code}, err)
}
