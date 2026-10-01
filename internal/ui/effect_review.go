package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewEffectOp uint8

const (
	reviewOpSave reviewEffectOp = iota
	reviewOpHandle
	reviewOpSend
	reviewOpStatus
	reviewOpNormalize
)

type reviewEffectRequest struct {
	op            reviewEffectOp
	targetID      string
	targetName    string
	repoRoot      string
	generation    int
	state         uireview.SavedState
	previousState uireview.SavedState
	commentID     string
	handled       bool
	previous      bool
	prompt        string
	commentIDs    []string
	previousRound uireview.Round
	round         int
	count         int
}

func (reviewEffectRequest) effectRequest() {}

type reviewEffectResult struct {
	op     reviewEffectOp
	save   uireview.SaveResult
	handle uireview.HandleCommentResult
	send   uireview.SendResult
	status uireview.StatusResult
}

func (reviewEffectResult) effectResult() {}

type reviewSessionWriter interface {
	exists(id string) (bool, error)
	sendText(id, text string) error
}

type driverReviewWriter struct{ drv *tmux.Driver }

func (w driverReviewWriter) exists(id string) (bool, error) { return w.drv.Exists(id), nil }
func (w driverReviewWriter) sendText(id, text string) error { return w.drv.SendText(id, text) }

func (s effectServices) runReview(request reviewEffectRequest) (effectResult, error) {
	writer := reviewSessionWriter(driverReviewWriter{drv: s.driver})
	return runReviewWithWriter(request, s.store, writer)
}

func runReviewWithWriter(request reviewEffectRequest, st *store.Store, writer reviewSessionWriter) (effectResult, error) {
	switch request.op {
	case reviewOpSave:
		return reviewEffectResult{op: reviewOpSave, save: uireview.SaveResult{
			TargetID: request.targetID, RepoRoot: request.repoRoot,
			Err: st.MergeReviewState(request.targetID, request.repoRoot, reviewStateToStore(request.state)),
		}}, nil
	case reviewOpHandle:
		found, err := st.SetReviewCommentHandled(request.targetID, request.commentID, request.handled)
		return reviewEffectResult{op: reviewOpHandle, handle: uireview.HandleCommentResult{
			TargetID: request.targetID, RepoRoot: request.repoRoot, CommentID: request.commentID,
			Handled: request.handled, Previous: request.previous, Found: found, Err: err,
		}}, nil
	case reviewOpStatus:
		state, err := st.ReviewState(request.targetID, request.repoRoot)
		result := uireview.StatusResult{TargetID: request.targetID, RepoRoot: request.repoRoot, Generation: request.generation, Err: err}
		if err == nil {
			result.Handled = make(map[string]bool, len(state.Comments))
			for _, comment := range state.Comments {
				if comment.ID != "" && comment.Round > 0 {
					result.Handled[comment.ID] = comment.Resolved
				}
			}
		}
		return reviewEffectResult{op: reviewOpStatus, status: result}, nil
	case reviewOpNormalize:
		current, err := st.ReviewState(request.targetID, request.repoRoot)
		if err != nil {
			return reviewEffectResult{op: reviewOpNormalize}, fmt.Errorf("persisting review normalization: %w", err)
		}
		normalized, changed := uireview.NormalizeSavedState(reviewStateFromStore(current))
		if !changed {
			return reviewEffectResult{op: reviewOpNormalize}, nil
		}
		err = st.MergeReviewState(request.targetID, request.repoRoot, reviewStateToStore(normalized))
		if err != nil {
			return reviewEffectResult{op: reviewOpNormalize}, fmt.Errorf("persisting review normalization: %w", err)
		}
		return reviewEffectResult{op: reviewOpNormalize}, nil
	case reviewOpSend:
		exists, probeErr := writer.exists(request.targetID)
		result := uireview.SendResult{
			TargetID: request.targetID, RepoRoot: request.repoRoot, CommentIDs: append([]string(nil), request.commentIDs...),
			PreviousRound: request.previousRound, Round: request.round, Count: request.count, TargetName: request.targetName,
		}
		if probeErr != nil {
			result.Err = probeErr
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		if !exists {
			result.Err = fmt.Errorf("%s", deadSessionHint)
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		if err := st.MergeReviewState(request.targetID, request.repoRoot, reviewStateToStore(request.state)); err != nil {
			result.Err = fmt.Errorf("saving review round: %w", err)
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		if err := writer.sendText(request.targetID, request.prompt); err != nil {
			result.Err = err
			if rollbackErr := st.MergeReviewState(request.targetID, request.repoRoot, reviewStateToStore(request.previousState)); rollbackErr != nil {
				result.Err = fmt.Errorf("%w; restoring review drafts: %w", err, rollbackErr)
			}
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		result.Delivered = true
		result.AckErr = st.SetAcked(request.targetID, false)
		return reviewEffectResult{op: reviewOpSend, send: result}, nil
	}
	panic(fmt.Sprintf("unknown review effect op %d", request.op))
}

func (m *Model) applyReviewEffect(job *effectJob, result reviewEffectResult, err error) tea.Cmd {
	switch result.op {
	case reviewOpSave:
		return m.handleReviewSave(result.save)
	case reviewOpHandle:
		return m.handleReviewComment(result.handle)
	case reviewOpSend:
		return m.handleReviewSend(result.send)
	case reviewOpStatus:
		return m.handleReviewStatus(result.status)
	}
	if err != nil {
		m.errBar.text = err.Error()
	}
	return nil
}
