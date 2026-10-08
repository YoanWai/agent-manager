package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type effectServices struct {
	engine                *status.Engine
	hooks                 *hooks.Manager
	installHomeDir        func() (string, error)
	installCheckInstalled func(string) error
	installSendText       func(*tmux.Driver, string, string) (tmux.SendResult, error)
	installSessionExists  func(*tmux.Driver, string) (bool, error)
	pasteInput            func(*tmux.Driver, string, string) error
	saveKeys              func(keybind.Table) error
	lifecycle             *sessioncmd.Lifecycle
	cfg                   config.Config
	store                 *store.Store
	driver                *tmux.Driver
	runner                *execution.Runner
	watch                 *focusWatch
	gitDrv                *git.Driver
	quickSessionExists    func(*tmux.Driver, string) (bool, error)
	quickSendText         func(*tmux.Driver, string, string) (tmux.SendResult, error)
}

func (m *Model) captureEffect(request effectRequest) func() (effectResult, error) {
	cfg := m.services.cfg
	cfg.Tools = make(map[string]config.Tool, len(m.services.cfg.Tools))
	for name, tool := range m.services.cfg.Tools {
		tool.Rules = append([]config.Rule(nil), tool.Rules...)
		cfg.Tools[name] = tool
	}
	services := effectServices{cfg: cfg, store: m.services.store, driver: m.services.tmux, watch: m.focus.runtime.watch, gitDrv: m.services.gitDrv}
	services.engine = m.services.engine
	services.hooks = m.services.hooks
	services.installHomeDir = installHomeDir
	services.installCheckInstalled = installCheckInstalled
	services.installSendText = installSendText
	services.installSessionExists = installSessionExists
	services.pasteInput = pasteFocused
	services.quickSessionExists = m.services.quickSessionExists
	if services.quickSessionExists == nil {
		services.quickSessionExists = func(driver *tmux.Driver, id string) (bool, error) { return driver.SessionExists(id) }
	}
	services.quickSendText = m.services.quickSendText
	if services.quickSendText == nil {
		services.quickSendText = func(driver *tmux.Driver, id, text string) (tmux.SendResult, error) {
			return driver.SendTextResult(id, text)
		}
	}
	services.saveKeys = m.services.saveKeys
	if services.saveKeys == nil {
		services.saveKeys = m.services.store.SetKeys
	}
	if m.poller != nil {
		services.runner = m.poller.runner
	}
	if m.services.lifecycle != nil {
		services.lifecycle = m.services.lifecycle.Capture(cfg, m.services.setSnapshot)
	}
	return func() (effectResult, error) {
		switch request := request.(type) {
		case inputRequest:
			return services.runInput(request)
		case quickSendRequest:
			return services.runQuickSend(request)
		case noticeDismissRequest:
			return services.runNoticeDismiss(request)
		case splitSaveRequest:
			return services.runSplitSave(request)
		case choiceSaveRequest:
			return services.runChoiceSave(request)
		case installStartRequest:
			return services.runInstallStart(request)
		case installSettleRequest:
			return services.runInstallSettle(request)
		case lifecycleRequest:
			return services.runLifecycle(request)
		case afterTurnRequest:
			return services.runAfterTurn(request)
		case railRequest:
			return services.runRail(request)
		case forkRequest:
			return services.runFork(request)
		case geometryRequest:
			return services.runGeometry(request)
		case spawnRequest:
			return services.runSpawn(request)
		case groupRequest:
			return services.runGroup(request)
		case renameRequest:
			return services.runRename(request)
		case moveDialogClose:
			return moveDialogCloseResult{}, nil
		case settingsRequest:
			return services.runSettings(request)
		case reviewEffectRequest:
			return services.runReview(request)
		case keysRequest:
			return services.runKeys(request)
		case focusRequest:
			return services.runFocus(request)
		case ackRequest:
			return services.runAck(request)
		case detachRequest:
			return services.runDetach(request)
		case attachRequest:
			return services.runAttach(request)
		}
		panic("unknown UI effect request")
	}
}

func (s effectServices) reflow(ids []string, work func()) error {
	if s.runner == nil {
		work()
		return nil
	}
	return s.runner.ReflowSessions(ids, work)
}
