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

// Run writes the dismissal the panel already shows.
func (r DismissRequest) Run(services Services) (uieffects.Result, error) {
	if services.Store == nil {
		return nil, fmt.Errorf("dismiss notice %q: store is unavailable", r.ID)
	}
	if err := services.Store.DismissNotice(r.ID); err != nil {
		return nil, fmt.Errorf("dismiss notice %q: %w", r.ID, err)
	}
	return nil, nil
}

// Queued reports whether a dismissal of id is already on the lane, running
// or waiting. requests is the lane's dismissals in order.
func Queued(requests []DismissRequest, id string) bool {
	for _, request := range requests {
		if request.ID == id {
			return true
		}
	}
	return false
}

// Dismissal is what a failed dismissal asks the root to do: report the
// error, and reopen the card on the notice only when the user has not moved
// on since accepting the dismissal.
type Dismissal struct {
	Restore string
	Reopen  string
	Err     error
}

// FailedDismissal decides a dismissal that did not persist. foreground is
// the generation the user's last key left, and clearList is whether no
// other card is up.
func FailedDismissal(request DismissRequest, err error, quitting bool, foreground uint64, cardOpen, clearList bool) Dismissal {
	out := Dismissal{Restore: request.ID, Err: err}
	if request.Modal && !quitting && request.ForegroundGen == foreground && (cardOpen || clearList) {
		out.Reopen = request.ID
	}
	return out
}
