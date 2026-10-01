package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type effectServices struct {
	lifecycle *sessioncmd.Lifecycle
	store     *store.Store
	driver    *tmux.Driver
	runner    *execution.Runner
	watch     *focusWatch
}

func (m *Model) captureEffect(request effectRequest) func() (effectResult, error) {
	cfg := m.services.cfg
	cfg.Tools = make(map[string]config.Tool, len(m.services.cfg.Tools))
	for name, tool := range m.services.cfg.Tools {
		tool.Rules = append([]config.Rule(nil), tool.Rules...)
		cfg.Tools[name] = tool
	}
	services := effectServices{store: m.services.store, driver: m.services.tmux, watch: m.focusRuntime.watch}
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
		case geometryRequest:
			return services.runGeometry(request)
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
