// Package source is the Review runtime: the git, SQLite and tmux work a Review
// load, draft save, comment mark, round send, base pick or picker read runs
// off the update path, and the return state Review hands back to the screen it
// was opened from. The Review model in package review decides what to ask for;
// the root queues the decision here and routes what comes back.
package source

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

// Op names the store or transport work a Request carries.
type Op uint8

const (
	Save Op = iota
	Handle
	Send
	Status
	Normalize
	SetBase
)

// Store is the rows the runtime reads and writes. Taking *store.Store is
// allowed here; the interface keeps the tests free of a real database.
type Store interface {
	ReviewState(sessionID, repoRoot string) (store.ReviewState, error)
	MergeReviewState(sessionID, repoRoot string, state store.ReviewState) error
	SetReviewCommentHandled(sessionID, commentID string, handled bool) (bool, error)
	ReviewBase(sessionID, repoRoot string) (string, error)
	SetReviewBase(sessionID, repoRoot, baseRef string) error
	Get(id string) (store.Session, error)
	SetAcked(id string, acked bool) error
}

// Writer is the tmux side of a round: the existence probe that runs before the
// store write, and the prompt the agent receives after it.
type Writer interface {
	Exists(id string) (bool, error)
	SendText(id, text string) (tmux.SendResult, error)
}

// SystemWriter is the tmux driver as a Writer.
type SystemWriter struct{ Driver *tmux.Driver }

func (w SystemWriter) Exists(id string) (bool, error) { return w.Driver.SessionExists(id) }

func (w SystemWriter) SendText(id, text string) (tmux.SendResult, error) {
	return w.Driver.SendTextResult(id, text)
}

// Request is one captured Review change. Every value the worker needs is
// copied in at dispatch, so an edit made after the decision cannot retarget
// work that is already queued.
type Request struct {
	Op            Op
	TargetID      string
	TargetName    string
	RepoRoot      string
	SourceRepo    string
	Generation    int
	BaseRef       string
	State         uireview.SavedState
	PreviousState uireview.SavedState
	CommentID     string
	Handled       bool
	Previous      bool
	Prompt        string
	CommentIDs    []string
	PreviousRound uireview.Round
	Round         int
	Count         int
	SendTarget    SendTarget
}

// Result is what the worker committed or refused, one field per op.
type Result struct {
	Op     Op
	Save   uireview.SaveResult
	Handle uireview.HandleCommentResult
	Send   uireview.SendResult
	Status uireview.StatusResult
	Base   BaseResult
}

// BaseResult reports a picked base that landed, with the review identity it
// was accepted for so a newer review cannot be retargeted by it.
type BaseResult struct {
	TargetID   string
	RepoRoot   string
	SourceRepo string
	Ref        string
	Generation int
	Err        error
}

// SendTarget is the session incarnation a round was accepted for. A relaunch
// in between replaces the pane, so the round must refuse rather than type into
// a stranger.
type SendTarget struct {
	ID         string
	Tool       string
	CreatedAt  time.Time
	LaunchTime time.Time
	Socket     string
}

// CaptureSendTarget records the session a round is about to be sent to.
func CaptureSendTarget(session store.Session) SendTarget {
	return SendTarget{
		ID: session.ID, Tool: session.Tool, CreatedAt: session.CreatedAt,
		LaunchTime: session.LaunchTime(), Socket: session.TmuxSocket,
	}
}

// Same reports whether current is still the session this target captured.
func (t SendTarget) Same(current store.Session) bool {
	return current.ID == t.ID && current.Tool == t.Tool &&
		current.CreatedAt.Equal(t.CreatedAt) && current.LaunchTime().Equal(t.LaunchTime) &&
		current.TmuxSocket == t.Socket
}
