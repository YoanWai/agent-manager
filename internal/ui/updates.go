package ui

import (
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m.requestQuit()
	}
	if m.effects.quitting {
		switch msg.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			return m, nil
		}
	}
	model, cmd := m.handleMsg(msg)
	if mm, ok := model.(*Model); ok {
		mm.flushPendingNotice()
		mm.prepareFrame()
		return mm, tea.Batch(cmd, mm.syncMouseCapture(), mm.refreshSpawnBase(), mm.nextEffectCmd())
	}
	m.prepareFrame()
	return model, tea.Batch(cmd, m.syncMouseCapture(), m.nextEffectCmd())
}

func (m *Model) handleMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	if model, cmd, ok := m.routeEffectMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeSpawnMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeSettingsMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeResizeMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeStartupMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routePreviewMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeRefreshMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeNoticesMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routePasteMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeFocusMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeSessionMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeReviewMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeEditorMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeStatusMsg(msg); ok {
		return model, cmd
	}
	if model, cmd, ok := m.routeConnectionMsg(msg); ok {
		return model, cmd
	}
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return m.handleMouse(msg)

	case uirail.AutoScrollTick:
		return m.applyRailDecision(m.rail.ApplyAutoScroll(msg, m.layout.displayedRail, m.railMouseContext()))

	case tea.KeyMsg:
		model, cmd := m.handleKey(msg)
		m.syncPollInput()
		return model, cmd
	}
	return m, nil
}

func routed(model tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd, bool) {
	return model, cmd, true
}
