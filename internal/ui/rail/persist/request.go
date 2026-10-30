package persist

import (
	"slices"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// Services is what a Rail change needs off the update path: the rows it
// writes and the panes whose labels that write leaves stale.
type Services struct {
	Store  Store
	Labels Labels
}

// Request is one Rail mutation, with the group metadata a move needs and the
// placement it must still find when it runs.
type Request struct {
	Mutation  uirail.Mutation
	Dir       string
	Worktree  string
	Base      string
	Env       string
	Placement Precondition
}

// Copied owns the slices its caller still holds.
func (r Request) Copied() Request {
	r.Mutation.Collapsed = slices.Clone(r.Mutation.Collapsed)
	r.Mutation.HostCollapsed = slices.Clone(r.Mutation.HostCollapsed)
	r.Mutation.GroupSiblings = slices.Clone(r.Mutation.GroupSiblings)
	return r
}

func (Request) EffectName() string { return "rail change" }

func (r Request) EffectCopy() uieffects.Request { return r.Copied() }

func (r Request) Run(s Services) (uieffects.Result, error) {
	return NewWriter(s.Store, s.Labels).Apply(r.Mutation, r.Placement)
}
