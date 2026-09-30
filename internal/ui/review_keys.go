package ui

import (
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ctx := uireview.KeyContext{CodeHeight: m.diffCodeHeight(), PersistenceAvailable: m.services.store != nil}
	if sess, ok := m.diffSession(); ok && m.isShell(sess.Tool) {
		ctx.IsShell = true
		ctx.SendError = shellPromptHint(sess.Name)
	}
	result := m.review.Key(msg, ctx)
	if result.Error != "" {
		m.errBar.text = result.Error
	}
	var navigation tea.Cmd
	switch result.Navigation {
	case uireview.NavigationQuit:
		navigation = tea.Quit
	case uireview.NavigationExit:
		navigation = m.closeDiff()
	case uireview.NavigationHelp:
		m.openHelp()
	case uireview.NavigationRepoPicker:
		m.openRepoPick()
	case uireview.NavigationBranchPicker:
		navigation = m.openBranchPick()
	case uireview.NavigationBasePicker:
		navigation = m.openBasePick()
	}
	return m, tea.Batch(navigation, m.reviewCommands(result.Requests))
}
