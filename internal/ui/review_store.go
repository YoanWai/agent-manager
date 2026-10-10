package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func readReviewState(stor *store.Store, sessionID, repoRoot string) (store.ReviewState, error) {
	state, err := stor.ReviewState(sessionID, repoRoot)
	if err != nil {
		return store.ReviewState{}, err
	}
	normalized, changed := uireview.NormalizeSavedState(reviewStateFromStore(state))
	if changed {
		state = reviewStateToStore(normalized)
		if err := stor.MergeReviewState(sessionID, repoRoot, state); err != nil {
			return store.ReviewState{}, err
		}
	}
	return state, nil
}

func (m *Model) handleReviewStatus(result uireview.StatusResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyStatus(result))
}

func (m *Model) handleReviewSave(result uireview.SaveResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplySave(result))
}

func (m *Model) handleReviewComment(result uireview.HandleCommentResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyHandle(result))
}

func (m *Model) handleReviewSend(result uireview.SendResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplySend(result))
}
