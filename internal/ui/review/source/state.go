package source

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

// StateFromStore reads a persisted review in the shape the Review model holds.
func StateFromStore(state store.ReviewState) uireview.SavedState {
	out := uireview.SavedState{Reviewed: state.Reviewed, Round: uireview.Round{Number: state.Round.Number, Scope: state.Round.Scope, Fingerprint: state.Round.Fingerprint}}
	out.Comments = make([]uireview.Comment, len(state.Comments))
	for i, note := range state.Comments {
		out.Comments[i] = uireview.Comment{ID: note.ID, File: note.File, Line: note.Line, Deleted: note.Deleted, Excerpt: note.Excerpt, Text: note.Text, ContentHash: note.ContentHash, Round: note.Round, Scope: note.Scope, Point: note.Point, Resolved: note.Resolved, Outdated: note.Outdated}
	}
	return out
}

// StateToStore writes a review in the shape the store persists.
func StateToStore(state uireview.SavedState) store.ReviewState {
	out := store.ReviewState{Reviewed: state.Reviewed, Round: store.ReviewRound{Number: state.Round.Number, Scope: state.Round.Scope, Fingerprint: state.Round.Fingerprint}}
	out.Comments = make([]store.ReviewComment, len(state.Comments))
	for i, note := range state.Comments {
		out.Comments[i] = store.ReviewComment{ID: note.ID, File: note.File, Line: note.Line, Deleted: note.Deleted, Excerpt: note.Excerpt, Text: note.Text, ContentHash: note.ContentHash, Round: note.Round, Scope: note.Scope, Point: note.Point, Resolved: note.Resolved, Outdated: note.Outdated}
	}
	return out
}

// ReadState reads one persisted review and rewrites it when the stored shape
// predates the current one. The rewrite is idempotent, so a load that failed
// to persist is fixed by the next load.
func ReadState(st Store, sessionID, repoRoot string) (store.ReviewState, error) {
	state, err := st.ReviewState(sessionID, repoRoot)
	if err != nil {
		return store.ReviewState{}, err
	}
	normalized, changed := uireview.NormalizeSavedState(StateFromStore(state))
	if changed {
		state = StateToStore(normalized)
		if err := st.MergeReviewState(sessionID, repoRoot, state); err != nil {
			return store.ReviewState{}, err
		}
	}
	return state, nil
}
