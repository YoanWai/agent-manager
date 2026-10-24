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
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	tea "github.com/charmbracelet/bubbletea"
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

// bindEffect captures the services request runs against: those of the
// feature package that declares it, or the root's own for a request still
// declared here.
func (m *Model) bindEffect(request uieffects.Request) func() (uieffects.Result, error) {
	capture := m.captureServices
	if work, ok := uieffects.Bind(request, func() uiinstall.Services { return capture().install() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uirailpersist.Services { return capture().railServices() }); ok {
		return work
	}
	services := capture()
	return func() (uieffects.Result, error) { return request.(effectRequest).run(services) }
}

// applyEffect applies a finished job's result on Update: a package result
// through its feature's host, a root result through its own apply.
func (m *Model) applyEffect(job *uieffects.Job, result uieffects.Result, err error) tea.Cmd {
	switch result := result.(type) {
	case uiinstall.StartResult:
		return m.ledger.install.Started(installRoot{m}, job.Request.(uiinstall.StartRequest), result, err)
	case uiinstall.SettleResult:
		return m.ledger.install.Settled(installRoot{m}, job.Request.(uiinstall.SettleRequest), result, err)
	case uirailpersist.ChangeResult:
		return m.applyRailEffect(result, job.Request.(uirailpersist.Request), job.Chain, err)
	case effectResult:
		return result.apply(m, job, err)
	}
	if err != nil {
		m.reportErr(err.Error())
	}
	return nil
}

func (m *Model) captureServices() effectServices {
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
	return services
}

func (s effectServices) reflow(ids []string, work func()) error {
	if s.runner == nil {
		work()
		return nil
	}
	return s.runner.ReflowSessions(ids, work)
}
