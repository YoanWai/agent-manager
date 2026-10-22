package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

type startupTickMsg struct{}

func (m *Model) startStartupTick() tea.Cmd {
	if m.startup.startupAnimating || !m.needsLoaderTick() {
		return nil
	}
	m.startup.startupAnimating = true
	return func() tea.Msg { return startupTickMsg{} }
}

func (m *Model) startupTick() tea.Cmd {
	return tea.Tick(startupInterval, func(time.Time) tea.Msg { return startupTickMsg{} })
}

type startupState struct {
	// sessionsSized flips after the first refresh shrinks sessions left
	// over from a previous manager run to the preview panel's width.
	sessionsSized bool
	// bannerPhase advances the wordmark's current sweep and then rests, so
	// the frame is not repainted forever.
	bannerPhase      int
	startupPhase     int
	startupAnimating bool
	booting          bool
	pendingTyped     map[string]typedPromptCandidate
}

func (m *Model) routeStartupMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case paneShellMsg:
		if msg.warning != "" {
			m.reportWarn(msg.warning)
		}
		return routed(m, nil)

	case bannerTickMsg:
		m.startup.bannerPhase++
		return routed(m, m.bannerTick())

	case bannerShimmerMsg:
		m.startup.bannerPhase = 0
		return routed(m, m.bannerTick())

	case startupTickMsg:
		if !m.needsLoaderTick() {
			m.startup.startupAnimating = false
			return routed(m, nil)
		}
		m.startup.startupPhase++
		return routed(m, m.startupTick())
	}
	return nil, nil, false
}

// paneShellMsg carries a warning about the pane shell, empty when it has
// none.
type paneShellMsg struct{ warning string }

// checkPaneShell runs the pane shell's version probe off the update path:
// it starts a PowerShell.
func checkPaneShell() tea.Msg {
	return paneShellMsg{warning: paneShellWarning()}
}
