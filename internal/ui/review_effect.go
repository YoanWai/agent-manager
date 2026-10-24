package ui

import (
	"maps"
	"slices"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewEffectRequest is one captured Review change on the effect lane.
type reviewEffectRequest struct {
	op            uireviewsource.Op
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
	sendTarget    uireviewsource.SendTarget
}

func (reviewEffectRequest) EffectName() string { return "review change" }

func (value reviewEffectRequest) EffectCopy() uieffects.Request {
	value.state.Reviewed = maps.Clone(value.state.Reviewed)
	value.state.Comments = slices.Clone(value.state.Comments)
	value.previousState.Reviewed = maps.Clone(value.previousState.Reviewed)
	value.previousState.Comments = slices.Clone(value.previousState.Comments)
	value.commentIDs = slices.Clone(value.commentIDs)
	return value
}

func (request reviewEffectRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runReview(request)
}

type reviewEffectResult struct {
	op     uireviewsource.Op
	save   uireview.SaveResult
	handle uireview.HandleCommentResult
	send   uireview.SendResult
	status uireview.StatusResult
	base   uireviewsource.BaseResult
}

func (reviewEffectResult) EffectResult() {}

func (result reviewEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyReviewEffect(job, result, err)
}

// driverReviewWriter is the tmux driver as the round's transport.
type driverReviewWriter struct{ drv *tmux.Driver }

func (w driverReviewWriter) Exists(id string) (bool, error) { return w.drv.SessionExists(id) }
func (w driverReviewWriter) SendText(id, text string) (tmux.SendResult, error) {
	return w.drv.SendTextResult(id, text)
}

func (s effectServices) runReview(request reviewEffectRequest) (uieffects.Result, error) {
	writer := uireviewsource.Writer(driverReviewWriter{drv: s.driver})
	return runReviewWithWriter(request, s.store, writer)
}

func runReviewWithWriter(request reviewEffectRequest, st *store.Store, writer uireviewsource.Writer) (uieffects.Result, error) {
	result, err := uireviewsource.Run(request.source(), st, writer)
	return reviewEffectResult{
		op: result.Op, save: result.Save, handle: result.Handle,
		send: result.Send, status: result.Status, base: result.Base,
	}, err
}

// source is this request in the runtime's spelling.
func (request reviewEffectRequest) source() uireviewsource.Request {
	return uireviewsource.Request{
		Op: request.op, TargetID: request.targetID, TargetName: request.targetName,
		RepoRoot: request.repoRoot, SourceRepo: request.sourceRepo, Generation: request.generation,
		BaseRef: request.baseRef, State: request.state, PreviousState: request.previousState,
		CommentID: request.commentID, Handled: request.handled, Previous: request.previous,
		Prompt: request.prompt, CommentIDs: request.commentIDs, PreviousRound: request.previousRound,
		Round: request.round, Count: request.count, SendTarget: request.sendTarget,
	}
}

func (m *Model) applyReviewEffect(job *uieffects.Job, result reviewEffectResult, err error) tea.Cmd {
	switch result.op {
	case uireviewsource.Save:
		return m.handleReviewSave(result.save)
	case uireviewsource.Handle:
		return m.handleReviewComment(result.handle)
	case uireviewsource.Send:
		return m.handleReviewSend(result.send)
	case uireviewsource.Status:
		return m.handleReviewStatus(result.status)
	case uireviewsource.SetBase:
		return m.applyReviewBase(result.base)
	}
	if err != nil {
		m.reportErr(err.Error())
	}
	return nil
}
