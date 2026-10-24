package notices

import (
	"fmt"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// Store is the persisted dismissal a queued one writes through.
type Store interface {
	DismissNotice(id string) error
}

// Services is what the notices effect lane captures for one job.
type Services struct{ Store Store }

// DismissRequest persists one optimistic dismissal through the shared FIFO
// effect lane. The foreground fence reopens the failed notice only when the
// user has not moved on since accepting the dismissal.
type DismissRequest struct {
	ID            string
	ForegroundGen uint64
	Modal         bool
}

func (DismissRequest) EffectName() string { return "notice dismissal" }

func (r DismissRequest) EffectCopy() uieffects.Request { return r }

// Run writes the dismissal the panel already shows.
func (r DismissRequest) Run(services Services) (uieffects.Result, error) {
	if services.Store == nil {
		return DismissResult{}, fmt.Errorf("dismiss notice %q: store is unavailable", r.ID)
	}
	if err := services.Store.DismissNotice(r.ID); err != nil {
		return DismissResult{}, fmt.Errorf("dismiss notice %q: %w", r.ID, err)
	}
	return DismissResult{}, nil
}

// DismissResult carries nothing back: a dismissal reports only its error.
type DismissResult struct{}

func (DismissResult) EffectResult() {}
