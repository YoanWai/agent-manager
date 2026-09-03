package sessioncmd

import (
	"context"
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
	message, err := Rename(t.Context(), configDir, "abc123", "fix auth bug")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if !strings.Contains(message, "queued") || strings.Contains(message, "renamed to") {
		t.Fatalf("with no manager running the answer must not claim the rename happened: %q", message)
	}
	if _, name, found := hooks.NewManager(configDir).ReadName("abc123"); !found || name != "fix auth bug" {
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
				request, pending, found, err := mailbox.ClaimName("abc123")
				if err == nil && found && pending == name {
					_ = mailbox.WriteNameResult("abc123", request, name, name, refusal)
					_ = mailbox.ReleaseName("abc123")
					return
				}
				if found {
					_ = mailbox.ReleaseName("abc123")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	answer(t, "fix auth bug", nil)
	// The typed name carries whitespace the manager squashes out, so the
	// wait has to recognize the answer by the squashed name.
	message, err := Rename(t.Context(), configDir, "abc123", "fix   auth bug")
	if err != nil || message != "session renamed to fix auth bug" {
		t.Fatalf("Rename = %q, %v", message, err)
	}
	if left, err := filepath.Glob(filepath.Join(mailbox.Dir(), "abc123.*.renamed")); err != nil || len(left) != 0 {
		t.Fatalf("a read answer must be consumed, left %v (%v)", left, err)
	}

	answer(t, "taken", errors.New("worktree rename: branch already exists: am/taken"))
	if _, err := Rename(t.Context(), configDir, "abc123", "taken"); err == nil || !strings.Contains(err.Error(), "branch already exists: am/taken") {
		t.Fatalf("a refusal must reach the caller with its reason, got %v", err)
	}
}

// Two renames for one session are in flight at once, so each caller is
// answered for the rename it queued and neither consumes the other's.
func TestRenameAnswersEachCallerItsOwnRename(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// The manager applies whichever rename it claims, and answers the
	// other one where its caller is looking too.
	// The claim is what the manager does: it takes the pending rename out
	// of the mailbox in one step, so a request queued meanwhile survives.
	go func() {
		answered := map[string]bool{}
		for len(answered) < 2 {
			request, pending, found, err := mailbox.ClaimName("abc123")
			if err != nil || !found {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			_ = mailbox.WriteNameResult("abc123", request, pending, pending, nil)
			_ = mailbox.ReleaseName("abc123")
			answered[request] = true
		}
	}()

	type outcome struct{ asked, message string }
	results := make(chan outcome, 2)
	for _, name := range []string{"first racer", "second racer"} {
		go func() {
			message, err := Rename(t.Context(), configDir, "abc123", name)
			if err != nil {
				message = "error: " + err.Error()
			}
			results <- outcome{name, message}
		}()
	}
	applied := 0
	for range 2 {
		got := <-results
		other := "first racer"
		if got.asked == other {
			other = "second racer"
		}
		if strings.Contains(got.message, other) {
			t.Fatalf("the caller that asked for %q was told about %q: %q", got.asked, other, got.message)
		}
		if got.message == "session renamed to "+got.asked {
			applied++
			continue
		}
		// A request the other one replaced in the mailbox was never
		// applied, and saying so is the honest answer.
		if !strings.Contains(got.message, "queued") {
			t.Fatalf("caller asking for %q got %q", got.asked, got.message)
		}
	}
	if applied == 0 {
		t.Fatal("neither rename was answered as applied")
	}
}

func TestRenameStopsWhenItsCallerGivesUp(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := Rename(ctx, configDir, "abc123", "abandoned"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rename = %v, want the cancellation", err)
	}
}

func TestRenameIgnoresAnAnswerToAnotherRequest(t *testing.T) {
	configDir := t.TempDir()
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mailbox.WriteNameResult("abc123", "earlier", "old name", "old name", nil); err != nil {
		t.Fatal(err)
	}
	message, err := Rename(t.Context(), configDir, "abc123", "new name")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if strings.Contains(message, "old name") || strings.Contains(message, "renamed to") {
		t.Fatalf("an answer to an earlier rename must not be read as this one: %q", message)
	}
	if _, found, err := mailbox.ReadNameResult("abc123", "earlier"); !found || err != nil {
		t.Fatalf("the earlier caller's answer was taken away: found=%v err=%v", found, err)
	}
}
