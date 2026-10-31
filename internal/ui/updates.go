package ui

import (
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && m.routeQuit(key) == quitNow {
		return m.requestQuit()
	}
	if m.effects.Quitting {
		switch msg.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			return m, nil
		}
	}
	model, cmd := m.handleMsg(msg)
	if mm, ok := model.(*Model); ok {
		mm.flushPendingNotice()
		mm.prepareFrame()
		mm.syncPointerShape()
		return mm, tea.Batch(cmd, mm.syncMouseCapture(), mm.refreshSpawnBase(), mm.nextEffectCmd())
	}
	m.prepareFrame()
	m.syncPointerShape()
	return model, tea.Batch(cmd, m.syncMouseCapture(), m.nextEffectCmd())
}

var messageRoutes = []func(*Model, tea.Msg) (tea.Model, tea.Cmd, bool){
	(*Model).routeEffectMsg,
	(*Model).routeSpawnMsg,
	(*Model).routeSetupMsg,
	(*Model).routeSettingsMsg,
	(*Model).routeResizeMsg,
	(*Model).routeStartupMsg,
	(*Model).routePreviewMsg,
	(*Model).routeRefreshMsg,
	(*Model).routeNoticesMsg,
	(*Model).routePasteMsg,
	(*Model).routeFocusMsg,
	(*Model).routeSessionMsg,
	(*Model).routeReviewMsg,
	(*Model).routeEditorMsg,
	(*Model).routeStatusMsg,
	(*Model).routeConnectionMsg,
}

func (m *Model) handleMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	for _, route := range messageRoutes {
		if model, cmd, ok := route(m, msg); ok {
			return model, cmd
		}
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
