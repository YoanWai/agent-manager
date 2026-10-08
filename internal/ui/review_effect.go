package ui

import (
	"errors"
	"fmt"
	"time"

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
	reviewOpSetBase
)

type reviewEffectRequest struct {
	op            reviewEffectOp
	targetID      string
	targetName    string
	repoRoot      string
	sourceRepo    string
	generation    int
	baseRef       string
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
	sendTarget    reviewSendTarget
}

func (reviewEffectRequest) effectRequest() {}

type reviewEffectResult struct {
	op     reviewEffectOp
	save   uireview.SaveResult
	handle uireview.HandleCommentResult
	send   uireview.SendResult
	status uireview.StatusResult
	base   reviewBaseResult
}

type reviewBaseResult struct {
	targetID   string
	repoRoot   string
	sourceRepo string
	ref        string
	generation int
	err        error
}

type reviewSendTarget struct {
	id         string
	tool       string
	createdAt  time.Time
	launchTime time.Time
	socket     string
}

func captureReviewSendTarget(session store.Session) reviewSendTarget {
	return reviewSendTarget{
		id: session.ID, tool: session.Tool, createdAt: session.CreatedAt,
		launchTime: session.LaunchTime(), socket: session.TmuxSocket,
	}
}

func sameReviewSendTarget(current store.Session, expected reviewSendTarget) bool {
	return current.ID == expected.id && current.Tool == expected.tool &&
		current.CreatedAt.Equal(expected.createdAt) && current.LaunchTime().Equal(expected.launchTime) &&
		current.TmuxSocket == expected.socket
}

func (reviewEffectResult) effectResult() {}

type reviewSessionWriter interface {
	exists(id string) (bool, error)
	sendText(id, text string) (tmux.SendResult, error)
}

type driverReviewWriter struct{ drv *tmux.Driver }

func (w driverReviewWriter) exists(id string) (bool, error) { return w.drv.SessionExists(id) }
func (w driverReviewWriter) sendText(id, text string) (tmux.SendResult, error) {
	return w.drv.SendTextResult(id, text)
}

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
	case reviewOpSetBase:
		return reviewEffectResult{op: reviewOpSetBase, base: reviewBaseResult{
			targetID: request.targetID, repoRoot: request.repoRoot, sourceRepo: request.sourceRepo,
			ref: request.baseRef, generation: request.generation,
			err: st.SetReviewBase(request.targetID, request.repoRoot, request.baseRef),
		}}, nil
	case reviewOpSend:
		result := uireview.SendResult{
			TargetID: request.targetID, RepoRoot: request.repoRoot, CommentIDs: append([]string(nil), request.commentIDs...),
			PreviousRound: request.previousRound, Round: request.round, Count: request.count, TargetName: request.targetName,
			Outcome: uireview.SendRefused,
		}
		current, targetErr := st.Get(request.targetID)
		if targetErr != nil {
			result.Err = fmt.Errorf("check review target identity: %w", targetErr)
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		if !sameReviewSendTarget(current, request.sendTarget) {
			result.Err = errors.New("review target changed its identity, tool, launch, or socket before the accepted round ran")
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		exists, probeErr := writer.exists(request.targetID)
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
		sent, sendErr := writer.sendText(request.targetID, request.prompt)
		if sendErr == nil && sent.Phase != tmux.SendPhaseSubmitted {
			sendErr = errors.New("review prompt was not confirmed submitted")
		}
		if sendErr != nil {
			result.Err = sendErr
			if sent.PasteMayHaveStarted() {
				result.Outcome = uireview.SendUncertain
				result.Err = fmt.Errorf("review round may have reached %s and was not sent again; inspect that pane before deciding what to do: %w", request.targetName, sendErr)
				return reviewEffectResult{op: reviewOpSend, send: result}, nil
			}
			if rollbackErr := st.MergeReviewState(request.targetID, request.repoRoot, reviewStateToStore(request.previousState)); rollbackErr != nil {
				result.Err = fmt.Errorf("%w; restoring review drafts: %w", sendErr, rollbackErr)
			}
			return reviewEffectResult{op: reviewOpSend, send: result}, nil
		}
		result.Outcome = uireview.SendConfirmed
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
	case reviewOpSetBase:
		return m.applyReviewBase(result.base)
	}
	if err != nil {
		m.reportErr(err.Error())
	}
	return nil
}
