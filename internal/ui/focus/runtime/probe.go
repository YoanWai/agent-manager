package runtime

import (
	"errors"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

type Prober struct {
	Store  *store.Store
	Driver *tmux.Driver
}

// Probe refuses a window tmux no longer holds, and clears the unread mark a
// finished session left, so a row entered after its turn does not read as new.
func (p Prober) Probe(sessionID string) error {
	if !p.Driver.Exists(sessionID) {
		return errors.New(uisession.DeadHint)
	}
	sess, err := p.Store.Get(sessionID)
	if err != nil {
		return err
	}
	if sess.Status == status.Finished {
		return p.Store.AcknowledgeFinished(sessionID)
	}
	return nil
}

func (p Prober) Ack(sessionID string) error { return p.Store.AcknowledgeFinished(sessionID) }

// Detach is what a session left behind when it asked to open Review or the
// editor and then detached.
type Detach struct {
	// Request is the marker the session wrote, empty when it wrote none.
	Request string
	// ClearErr is a failed clear, which leaves the marker for the next read.
	ClearErr error
}

// Detached reads and clears the request a session left before it detached.
func (p Prober) Detached(sessionID string) (Detach, error) {
	marker, err := p.Driver.PendingRequest(sessionID)
	if err != nil {
		return Detach{}, err
	}
	if marker == "" {
		return Detach{}, nil
	}
	return Detach{Request: marker, ClearErr: p.Driver.ClearRequest(sessionID)}, nil
}
