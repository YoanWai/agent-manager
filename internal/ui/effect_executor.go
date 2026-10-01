package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type effectServices struct {
	lifecycle *sessioncmd.Lifecycle
	cfg       config.Config
	store     *store.Store
	driver    *tmux.Driver
	runner    *execution.Runner
	watch     *focusWatch
	gitDrv    *git.Driver
}

func (m *Model) captureEffect(request effectRequest) func() (effectResult, error) {
	cfg := m.services.cfg
	cfg.Tools = make(map[string]config.Tool, len(m.services.cfg.Tools))
	for name, tool := range m.services.cfg.Tools {
		tool.Rules = append([]config.Rule(nil), tool.Rules...)
		cfg.Tools[name] = tool
	}
	services := effectServices{cfg: cfg, store: m.services.store, driver: m.services.tmux, watch: m.focusRuntime.watch, gitDrv: m.services.gitDrv}
	if m.poller != nil {
		services.runner = m.poller.runner
	}
	if m.services.lifecycle != nil {
		services.lifecycle = m.services.lifecycle.Capture(cfg, m.services.setSnapshot)
	}
	return func() (effectResult, error) {
		switch request := request.(type) {
		case lifecycleRequest:
			return services.runLifecycle(request)
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
