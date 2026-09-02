package sessioncmd

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
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

func stampManagerHeartbeat(t *testing.T, configDir string) {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSetting(store.PollerHeartbeatKey, strconv.FormatInt(time.Now().UnixNano(), 10)); err != nil {
		t.Fatal(err)
	}
}

func TestRenameWithoutAManagerReportsItQueued(t *testing.T) {
	configDir := t.TempDir()
	message, err := Rename(configDir, "abc123", "fix auth bug")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if !strings.Contains(message, "queued") || strings.Contains(message, "renamed to") {
		t.Fatalf("with no manager running the answer must not claim the rename happened: %q", message)
	}
	if name, found := hooks.NewManager(configDir).ReadName("abc123"); !found || name != "fix auth bug" {
		t.Fatalf("name file = %q, %v; the rename must still wait for a manager", name, found)
	}
}

// The subcommand answers with what the poller did, so an agent that
// names itself reasons about the name it actually has.
func TestRenameWaitsForTheManagerAndReportsItsAnswer(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	mailbox := hooks.NewManager(configDir)
	answer := func(t *testing.T, name string, refusal error) {
		t.Helper()
		go func() {
			for {
				if pending, found := mailbox.ReadName("abc123"); found && pending == name {
					_ = mailbox.WriteNameResult("abc123", name, refusal)
					_ = mailbox.RemoveName("abc123")
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	answer(t, "fix auth bug", nil)
	message, err := Rename(configDir, "abc123", "fix auth bug")
	if err != nil || message != "session renamed to fix auth bug" {
		t.Fatalf("Rename = %q, %v", message, err)
	}
	if _, _, found := mailbox.ReadNameResult("abc123"); found {
		t.Fatal("a read answer must be consumed")
	}

	answer(t, "taken", errors.New("worktree rename: branch already exists: am/taken"))
	if _, err := Rename(configDir, "abc123", "taken"); err == nil || !strings.Contains(err.Error(), "branch already exists: am/taken") {
		t.Fatalf("a refusal must reach the caller with its reason, got %v", err)
	}
}

func TestRenameDiscardsAStaleAnswer(t *testing.T) {
	configDir := t.TempDir()
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mailbox.WriteNameResult("abc123", "old name", nil); err != nil {
		t.Fatal(err)
	}
	message, err := Rename(configDir, "abc123", "new name")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if strings.Contains(message, "old name") || strings.Contains(message, "renamed to") {
		t.Fatalf("an answer left over from an earlier rename must not be read as this one: %q", message)
	}
}
