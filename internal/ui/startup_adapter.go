package ui

import (
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	tea "github.com/charmbracelet/bubbletea"
)

// startupState is the loader, the wordmark sweep and the boot ring: the
// package owns the timing and the frame counter, the root the fields the
// rail, preview and layout read every frame.
type startupState = uistartup.State

func (m *Model) routeStartupMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	cmd, ok := m.startup.Update(startupRoot{m}, msg)
	if !ok {
		return nil, nil, false
	}
	return routed(m, cmd)
}

// startupRoot is the root as the startup animation reads it.
type startupRoot struct{ *Model }

func (h startupRoot) LoaderNeeded() bool { return h.needsLoaderTick() }

func (h startupRoot) ReportWarn(text string) { h.reportWarn(text) }

func (m *Model) startStartupTick() tea.Cmd { return m.startup.StartTick(startupRoot{m}) }

func (m *Model) bannerTick() tea.Cmd { return m.startup.BannerTick() }
