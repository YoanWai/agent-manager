package source

import (
	"maps"
	"slices"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// Change is one captured Review change on the effect lane: the store and
// transport work Run performs, spelled as the job that carries it.
type Change struct {
	Request
}

// Copied owns its maps and slices, so an edit to the Review the decision read
// cannot retarget a job that is already queued.
func (c Change) Copied() Change {
	c.State.Reviewed = maps.Clone(c.State.Reviewed)
	c.State.Comments = slices.Clone(c.State.Comments)
	c.PreviousState.Reviewed = maps.Clone(c.PreviousState.Reviewed)
	c.PreviousState.Comments = slices.Clone(c.PreviousState.Comments)
	c.CommentIDs = slices.Clone(c.CommentIDs)
	return c
}

func (Change) EffectName() string { return "review change" }

func (c Change) EffectCopy() uieffects.Request { return c.Copied() }

// Run keeps the Op it was asked for even when the work failed: the root
// applies the result by op, and a failed normalization is not a failed save.
func (c Change) Run(services Services) (uieffects.Result, error) {
	result, err := Run(c.Request, services.Store, services.Writer)
	return ChangeResult(result), err
}

// ChangeResult is what Change committed or refused, one field per Op.
type ChangeResult Result

func (ChangeResult) EffectResult() {}

// Services is what one Review change runs against: the rows it reads and
// writes, and the pane a round is typed into.
type Services struct {
	Store  *store.Store
	Writer Writer
}

// DriverWriter is the tmux driver as the round's transport.
func DriverWriter(driver *tmux.Driver) Writer { return SystemWriter{Driver: driver} }

// PendingSetBase reports whether any of jobs saves a diff base.
func PendingSetBase(jobs ...*uieffects.Job) bool {
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if change, ok := job.Request.(Change); ok && change.Op == SetBase {
			return true
		}
	}
	return false
}

// IsSend reports whether request types into an agent's pane, which makes a
// listing captured before it finished stale.
func IsSend(request uieffects.Request) bool {
	change, ok := request.(Change)
	return ok && change.Op == Send
}
