package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestReviewStatePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ReviewState{
		Reviewed: map[string]uint64{"main.go": 42},
		Comments: []ReviewComment{{
			ID: "0123456789abcdef", File: "main.go", Line: 7, Excerpt: "return value", Text: "check the error",
			ContentHash: 42, Round: 2, Point: 1, Outdated: true,
		}},
		Round: ReviewRound{Number: 2, Scope: "unstaged", Fingerprint: 99},
	}
	if err := st.SetReviewState("session", "/repo", want); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.ReviewState("session", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("review state = %#v, want %#v", got, want)
	}
}

func TestReviewCommentHandledKeepsTheComment(t *testing.T) {
	st := newTestStore(t)
	want := ReviewComment{
		ID: "0123456789abcdef", File: "main.go", Line: 7, Text: "check the error", Round: 2, Point: 1,
	}
	if err := st.SetReviewState("session", "/repo", ReviewState{Comments: []ReviewComment{want}}); err != nil {
		t.Fatal(err)
	}
	if found, err := st.SetReviewCommentHandled("session", want.ID, true); err != nil || !found {
		t.Fatalf("mark handled = %v, %v", found, err)
	}
	state, err := st.ReviewState("session", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || !state.Comments[0].Resolved {
		t.Fatalf("handled comment should remain in history: %+v", state.Comments)
	}
	if found, err := st.SetReviewCommentHandled("session", want.ID, false); err != nil || !found {
		t.Fatalf("reopen = %v, %v", found, err)
	}
	state, err = st.ReviewState("session", "/repo")
	if err != nil || len(state.Comments) != 1 || state.Comments[0].Resolved {
		t.Fatalf("reopened comment = %+v, %v", state.Comments, err)
	}
	if found, err := st.SetReviewCommentHandled("session", "fedcba9876543210", true); err != nil || found {
		t.Fatalf("unknown comment = %v, %v", found, err)
	}
}

func TestReviewCommentHandledRejectsDuplicateIDs(t *testing.T) {
	st := newTestStore(t)
	const commentID = "0123456789abcdef"
	for _, root := range []string{"/one", "/two"} {
		if err := st.SetReviewState("session", root, ReviewState{Comments: []ReviewComment{{
			ID: commentID, File: "main.go", Line: 1, Text: "fix this", Round: 1, Point: 1,
		}}}); err != nil {
			t.Fatal(err)
		}
	}
	if found, err := st.SetReviewCommentHandled("session", commentID, true); err == nil || found {
		t.Fatalf("duplicate comment id = found %v, err %v", found, err)
	}
	for _, root := range []string{"/one", "/two"} {
		state, err := st.ReviewState("session", root)
		if err != nil || state.Comments[0].Resolved {
			t.Fatalf("%s changed after ambiguous update: %+v, %v", root, state, err)
		}
	}
}

func TestMergeReviewStatePreservesAConcurrentHandledUpdate(t *testing.T) {
	st := newTestStore(t)
	const commentID = "0123456789abcdef"
	if err := st.SetReviewState("session", "/repo", ReviewState{Comments: []ReviewComment{{
		ID: commentID, File: "main.go", Line: 7, Text: "fix this", Round: 1, Point: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	if found, err := st.SetReviewCommentHandled("session", commentID, true); err != nil || !found {
		t.Fatalf("mark handled = %v, %v", found, err)
	}
	if err := st.MergeReviewState("session", "/repo", ReviewState{Comments: []ReviewComment{
		{ID: commentID, File: "main.go", Line: 9, Text: "fix this", Round: 1, Point: 1},
		{ID: "fedcba9876543210", File: "main.go", Line: 12, Text: "draft"},
	}}); err != nil {
		t.Fatal(err)
	}
	state, err := st.ReviewState("session", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 2 || !state.Comments[0].Resolved || state.Comments[0].Line != 9 {
		t.Fatalf("merged state = %+v", state.Comments)
	}
}

func TestEmptyReviewStateDeletesRow(t *testing.T) {
	st := newTestStore(t)
	if err := st.SetReviewState("session", "/repo", ReviewState{
		Reviewed: map[string]uint64{"main.go": 42},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewState("session", "/repo", ReviewState{}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM review_states`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("review state rows = %d, want 0", count)
	}
	if err := st.SetReviewState("session", "/repo", ReviewState{
		Reviewed: map[string]uint64{"main.go": 42},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.MergeReviewState("session", "/repo", ReviewState{}); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM review_states`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("review state rows after merge = %d, want 0", count)
	}
}

func TestDeleteSessionCleansReviewState(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("session", "group")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewState("session", "/repo", ReviewState{
		Round: ReviewRound{Number: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("session"); err != nil {
		t.Fatal(err)
	}
	got, err := st.ReviewState("session", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, ReviewState{}) {
		t.Fatalf("review state after delete = %#v", got)
	}
}
