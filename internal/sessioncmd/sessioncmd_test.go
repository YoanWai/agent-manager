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

func reviewCommentResolved(t *testing.T, configDir, repoRoot string) bool {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	state, err := st.ReviewState("abc123", repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 {
		t.Fatalf("review state for %s = %+v, want the one comment", repoRoot, state.Comments)
	}
	return state.Comments[0].Resolved
}

// The same id under two repos is the one case the store refuses outright,
// since it cannot tell which comment the agent meant.
func reviewCommentInASecondRepo(t *testing.T, configDir string) {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewState("abc123", "/other", store.ReviewState{Comments: []store.ReviewComment{{
		ID: reviewTestComment, Round: 1, Point: 1, Text: "fix this too",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
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
	if !reviewCommentResolved(t, configDir, "/repo") {
		t.Fatal("the comment was not stored as handled")
	}

	message, err = ReviewComment(configDir, "abc123", reviewTestComment, false)
	if err != nil {
		t.Fatal(err)
	}
	if message != "review comment "+reviewTestComment+" reopened" {
		t.Fatalf("reopen message = %q", message)
	}
	if reviewCommentResolved(t, configDir, "/repo") {
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
	if reviewCommentResolved(t, configDir, "/repo") {
		t.Fatal("a rejected id still changed the stored comment")
	}
}

func TestReviewCommentRefusesAnIDTwoReposShare(t *testing.T) {
	configDir := reviewConfigDir(t)
	reviewCommentInASecondRepo(t, configDir)

	_, err := ReviewComment(configDir, "abc123", reviewTestComment, true)
	if err == nil {
		t.Fatal("an id two repos share was accepted")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v, want the ambiguity refusal", err)
	}
	for _, repoRoot := range []string{"/repo", "/other"} {
		if reviewCommentResolved(t, configDir, repoRoot) {
			t.Fatalf("the refused call still marked %s handled", repoRoot)
		}
	}
}
