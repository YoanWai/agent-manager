package ui

import (
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

// themeHost is the root as a pane-theme push reports back to it.
type themeHost struct{ m *Model }

func (h themeHost) ReportErr(text string) { h.m.reportErr(text) }

func (h themeHost) Services() uitheme.Services {
	return uitheme.Services{Tmux: h.m.services.tmux}
}
