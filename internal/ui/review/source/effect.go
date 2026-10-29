package source

import (
	"errors"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/tmux"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

// Run performs one captured Review change and returns what it committed.
// An error means the job itself could not run, which is reported as is; a
// refusal the user can act on is a Result instead.
func Run(request Request, st Store, writer Writer) (Result, error) {
	switch request.Op {
	case Save:
		return Result{Op: Save, Save: uireview.SaveResult{
			TargetID: request.TargetID, RepoRoot: request.RepoRoot,
			Err: st.MergeReviewState(request.TargetID, request.RepoRoot, StateToStore(request.State)),
		}}, nil
	case Handle:
		found, err := st.SetReviewCommentHandled(request.TargetID, request.CommentID, request.Handled)
		return Result{Op: Handle, Handle: uireview.HandleCommentResult{
			TargetID: request.TargetID, RepoRoot: request.RepoRoot, CommentID: request.CommentID,
			Handled: request.Handled, Previous: request.Previous, Found: found, Err: err,
		}}, nil
	case Status:
		state, err := st.ReviewState(request.TargetID, request.RepoRoot)
		result := uireview.StatusResult{TargetID: request.TargetID, RepoRoot: request.RepoRoot, Generation: request.Generation, Err: err}
		if err == nil {
			result.Handled = make(map[string]bool, len(state.Comments))
			for _, comment := range state.Comments {
				if comment.ID != "" && comment.Round > 0 {
					result.Handled[comment.ID] = comment.Resolved
				}
			}
		}
		return Result{Op: Status, Status: result}, nil
	case Normalize:
		current, err := st.ReviewState(request.TargetID, request.RepoRoot)
		if err != nil {
			return Result{Op: Normalize}, fmt.Errorf("persisting review normalization: %w", err)
		}
		normalized, changed := uireview.NormalizeSavedState(StateFromStore(current))
		if !changed {
			return Result{Op: Normalize}, nil
		}
		err = st.MergeReviewState(request.TargetID, request.RepoRoot, StateToStore(normalized))
		if err != nil {
			return Result{Op: Normalize}, fmt.Errorf("persisting review normalization: %w", err)
		}
		return Result{Op: Normalize}, nil
	case SetBase:
		return Result{Op: SetBase, Base: BaseResult{
			TargetID: request.TargetID, RepoRoot: request.RepoRoot, SourceRepo: request.SourceRepo,
			Ref: request.BaseRef, Generation: request.Generation,
			Err: st.SetReviewBase(request.TargetID, request.RepoRoot, request.BaseRef),
		}}, nil
	case Send:
		return runSend(request, st, writer)
	}
	panic(fmt.Sprintf("unknown review op %d", request.Op))
}

// runSend persists the round and types the prompt in one ordered job: the
// existence probe, state merge, delivery, rollback and ack clear all run
// behind every earlier Review mutation.
func runSend(request Request, st Store, writer Writer) (Result, error) {
	result := uireview.SendResult{
		TargetID: request.TargetID, RepoRoot: request.RepoRoot, CommentIDs: append([]string(nil), request.CommentIDs...),
		PreviousRound: request.PreviousRound, Round: request.Round, Count: request.Count, TargetName: request.TargetName,
		Outcome: uireview.SendRefused,
	}
	current, targetErr := st.Get(request.TargetID)
	if targetErr != nil {
		result.Err = fmt.Errorf("check review target identity: %w", targetErr)
		return Result{Op: Send, Send: result}, nil
	}
	if !request.SendTarget.Same(current) {
		result.Err = errors.New("review target changed its identity, tool, launch, or socket before the accepted round ran")
		return Result{Op: Send, Send: result}, nil
	}
	exists, probeErr := writer.Exists(request.TargetID)
	if probeErr != nil {
		result.Err = probeErr
		return Result{Op: Send, Send: result}, nil
	}
	if !exists {
		keys, keysErr := st.ListKeys()
		if keysErr != nil {
			result.Err = keysErr
			return Result{Op: Send, Send: result}, nil
		}
		result.Err = uisession.DeadError(current.Archived, keys)
		return Result{Op: Send, Send: result}, nil
	}
	if err := st.MergeReviewState(request.TargetID, request.RepoRoot, StateToStore(request.State)); err != nil {
		result.Err = fmt.Errorf("saving review round: %w", err)
		return Result{Op: Send, Send: result}, nil
	}
	sent, sendErr := writer.SendText(request.TargetID, request.Prompt)
	if sendErr == nil && sent.Phase != tmux.SendPhaseSubmitted {
		sendErr = errors.New("review prompt was not confirmed submitted")
	}
	if sendErr != nil {
		result.Err = sendErr
		if sent.PasteMayHaveStarted() {
			result.Outcome = uireview.SendUncertain
			result.Err = fmt.Errorf("review round may have reached %s and was not sent again; inspect that pane before deciding what to do: %w", request.TargetName, sendErr)
			return Result{Op: Send, Send: result}, nil
		}
		if rollbackErr := st.MergeReviewState(request.TargetID, request.RepoRoot, StateToStore(request.PreviousState)); rollbackErr != nil {
			result.Err = fmt.Errorf("%w; restoring review drafts: %w", sendErr, rollbackErr)
		}
		return Result{Op: Send, Send: result}, nil
	}
	result.Outcome = uireview.SendConfirmed
	result.AckErr = st.SetAcked(request.TargetID, false)
	return Result{Op: Send, Send: result}, nil
}
