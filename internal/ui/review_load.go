package ui

import (
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) applyReviewResult(result uireview.ApplyResult) tea.Cmd {
	if !result.Accepted {
		return nil
	}
	if result.Error != "" {
		m.errBar.text = result.Error
	}
	if result.ForgetPreferredRepo != "" && m.ledger.pickedRepos[m.review.SessionID()] == result.ForgetPreferredRepo {
		delete(m.ledger.pickedRepos, m.review.SessionID())
	}
	if result.Refresh {
		m.requestRefresh()
	}
	return m.reviewCommands(result.Requests)
}

func (m *Model) handleReviewLoad(result uireview.LoadResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyLoad(result))
}

func (m *Model) handleReviewFile(result uireview.FileResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyFile(result))
}

func (m *Model) handleReviewProbe(result uireview.ProbeResult) tea.Cmd {
	request, ok := m.review.ApplyProbe(result)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}

func (m *Model) cycleDiffScope() tea.Cmd {
	request, ok := m.review.CycleScope()
	if !ok {
		return nil
	}
	return tea.Batch(m.reviewLoadCmd(request), m.startStartupTick())
}

func (m *Model) diffRefreshCmd() tea.Cmd {
	if m.services.gitDrv == nil {
		return nil
	}
	request, ok := m.review.RefreshRequest()
	if !ok {
		return nil
	}
	return m.reviewProbeCmd(request)
}

func (m *Model) reviewStatusesCmd() tea.Cmd {
	if m.services.store == nil {
		return nil
	}
	request, ok := m.review.StatusRequest()
	if !ok {
		return nil
	}
	return m.reviewStatusCmd(request)
}

func (m *Model) loadCurrentDiffFile() tea.Cmd {
	return m.reviewCommands(m.review.EnsureCurrentFile())
}

func (m *Model) ensureHighlight() tea.Cmd {
	request, ok := m.review.EnsureHighlight()
	if !ok {
		return nil
	}
	return m.reviewHighlightCmd(request)
}
