package ui

import (
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
)

// install hands the install runtime the services it runs with.
func (s effectServices) install() uiinstall.Services {
	return uiinstall.Services{
		Lifecycle: s.lifecycle,
		Hooks:     s.hooks,
		Driver:    s.driver,
		Tools:     s.cfg.Tools,
	}
}
