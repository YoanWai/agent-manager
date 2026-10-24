package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
)

type effectServices struct {
	engine             *status.Engine
	hooks              *hooks.Manager
	pasteInput         func(*tmux.Driver, string, string) error
	saveKeys           func(keybind.Table) error
	lifecycle          *sessioncmd.Lifecycle
	cfg                config.Config
	store              *store.Store
	driver             *tmux.Driver
	runner             *execution.Runner
	watch              *focuswatch.Watcher
	gitDrv             *git.Driver
	quickSessionExists func(*tmux.Driver, string) (bool, error)
	quickSendText      func(*tmux.Driver, string, string) (tmux.SendResult, error)
	remote             *remote.Client
}

func (m *Model) captureEffect(request effectRequest) func() (uieffects.Result, error) {
	cfg := m.services.cfg
	cfg.Tools = make(map[string]config.Tool, len(m.services.cfg.Tools))
	for name, tool := range m.services.cfg.Tools {
		tool.Rules = append([]config.Rule(nil), tool.Rules...)
		cfg.Tools[name] = tool
	}
	services := effectServices{cfg: cfg, store: m.services.store, driver: m.services.tmux, watch: m.focus.runtime.Watch, gitDrv: m.services.gitDrv, remote: m.ssh.Client}
	services.engine = m.services.engine
	services.hooks = m.services.hooks
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
	return func() (uieffects.Result, error) { return request.run(services) }
}

func (s effectServices) reflow(ids []string, work func()) error {
	if s.runner == nil {
		work()
		return nil
	}
	return s.runner.ReflowSessions(ids, work)
}
