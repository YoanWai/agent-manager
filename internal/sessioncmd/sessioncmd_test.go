package sessioncmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

const reviewTestComment = "0123456789abcdef"

func reviewConfigDir(t *testing.T) string {
	t.Helper()
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewState("abc123", "/repo", store.ReviewState{Comments: []store.ReviewComment{{
		ID: reviewTestComment, Round: 1, Point: 1, Text: "fix this",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return configDir
}

func reviewCommentResolved(t *testing.T, configDir string) bool {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	state, err := st.ReviewState("abc123", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 {
		t.Fatalf("review state = %+v, want the one comment", state.Comments)
	}
	return state.Comments[0].Resolved
}

func TestReviewCommentMarksHandledAndReopens(t *testing.T) {
	configDir := reviewConfigDir(t)

	message, err := ReviewComment(configDir, "abc123", reviewTestComment, true)
	if err != nil {
		t.Fatal(err)
	}
	if message != "review comment "+reviewTestComment+" marked handled" {
		t.Fatalf("message = %q", message)
	}
	if !reviewCommentResolved(t, configDir) {
		t.Fatal("the comment was not stored as handled")
	}

	message, err = ReviewComment(configDir, "abc123", reviewTestComment, false)
	if err != nil {
		t.Fatal(err)
	}
	if message != "review comment "+reviewTestComment+" reopened" {
		t.Fatalf("reopen message = %q", message)
	}
	if reviewCommentResolved(t, configDir) {
		t.Fatal("the comment was not stored as open again")
	}
}

func TestReviewCommentRejectsAnIDItCannotUse(t *testing.T) {
	configDir := reviewConfigDir(t)
	unknown := "fedcba9876543210"

	for _, id := range []string{"", "  ", "nope", strings.ToUpper(reviewTestComment), reviewTestComment + "0"} {
		if _, err := ReviewComment(configDir, "abc123", id, true); err == nil {
			t.Fatalf("%q was accepted as a comment id", id)
		}
	}
	if _, err := ReviewComment(configDir, "abc123", unknown, true); err == nil {
		t.Fatal("an id belonging to no comment was accepted")
	}
	if reviewCommentResolved(t, configDir) {
		t.Fatal("a rejected id still changed the stored comment")
	}
}
