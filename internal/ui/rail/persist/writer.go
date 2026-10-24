package persist

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// Precondition is the placement the caller saw when it queued the move: a
// row that changed underneath an open dialog must not be placed from the
// stale target.
type Precondition struct {
	Verify         bool
	SourceGroup    string
	SourceParentID string
	TargetID       string
	TargetGroup    string
}

// Result is what a committed change reports back: the rows whose group or
// parent moved, the group path a move produced, and a label failure worth a
// status line.
type Result struct {
	Sessions []store.Session
	Warning  error
	NewPath  string
}

// Writer persists one Rail mutation and reads back the rows it changed.
type Writer struct {
	store  Store
	labels Labels
}

func NewWriter(st Store, labels Labels) Writer {
	return Writer{store: st, labels: labels}
}

// Apply writes mutation, refusing a placement whose source or target moved
// since the caller captured it.
func (w Writer) Apply(mutation uirail.Mutation, placement Precondition) (Result, error) {
	result := Result{}
	if placement.Verify && mutation.Kind == uirail.PlaceSession {
		source, err := w.store.Get(mutation.SessionID)
		if err != nil {
			return result, err
		}
		if source.Group != placement.SourceGroup || source.ParentID != placement.SourceParentID {
			return result, fmt.Errorf("move source changed before placement")
		}
		if placement.TargetID != "" {
			target, err := w.store.Get(placement.TargetID)
			if err != nil {
				return result, err
			}
			if target.Group != placement.TargetGroup {
				return result, fmt.Errorf("move target changed before placement")
			}
		}
		if source.ParentID == mutation.ParentID && source.Group == mutation.Group {
			result.Sessions = []store.Session{source}
			return result, nil
		}
	}
	var err error
	switch mutation.Kind {
	case uirail.SaveCollapsed:
		err = SaveFolds(w.store, mutation.Collapsed, mutation.HostCollapsed)
	case uirail.SwapSession:
		err = w.store.SwapSessionOrder(mutation.SessionID, mutation.TargetID)
	case uirail.SwapGroup:
		err = w.store.SwapGroupOrder(mutation.Path, mutation.TargetPath, mutation.GroupSiblings...)
	case uirail.PlaceSession:
		err = w.store.PlaceSession(mutation.SessionID, mutation.Group, mutation.ParentID)
	case uirail.PlaceSessionBefore:
		err = w.store.PlaceSessionBefore(mutation.SessionID, mutation.TargetID)
	case uirail.MoveGroup:
		err = w.store.MoveGroup(mutation.Path, mutation.Group)
		result.NewPath = uirail.BaseName(mutation.Path)
		if mutation.Group != "" {
			result.NewPath = mutation.Group + "/" + result.NewPath
		}
	}
	if err != nil {
		return result, err
	}
	switch mutation.Kind {
	case uirail.PlaceSession, uirail.PlaceSessionBefore:
		sess, err := w.store.Get(mutation.SessionID)
		if err == nil {
			result.Sessions = append(result.Sessions, sess)
			var children []store.Session
			children, err = w.store.Children(sess.ID)
			result.Sessions = append(result.Sessions, children...)
		}
		if err != nil {
			result.Warning = err
		}
	case uirail.MoveGroup:
		sessions, err := w.store.SessionsInSubtree(result.NewPath)
		result.Sessions = sessions
		result.Warning = err
	}
	// Persistence already succeeded; a label failure is a warning.
	for _, sess := range result.Sessions {
		if !w.labels.Exists(sess.ID) {
			continue
		}
		if err := w.labels.SetLabel(sess.ID, sessioncmd.SessionLabel(sess.Group, sess.Name)); err != nil && result.Warning == nil {
			result.Warning = err
		}
	}
	return result, nil
}
