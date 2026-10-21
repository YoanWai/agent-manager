package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

// The scope picked with s is the one the session's review opens in next,
// after a restart too, while a session never moved off the default keeps it.
func TestReviewRemembersScopePerSession(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openReviewOn(t, m, "stack", dir)
	sess, _ := m.diffSession()
	m.enterCommitScope(t)
	if stored, err := m.services.store.ReviewScope(sess.ID); err != nil || stored != "commit" {
		t.Fatalf("stored scope = %q, %v, want commit", stored, err)
	}

	m.drainCmds(t, m.closeDiff())
	// A restart forgets everything review held in memory.
	m.review = uireview.New(false)
	m.selectSessionRow(t, "stack")
	m.drainCmds(t, m.openDiff())
	if got := m.review.Scope(); got != git.ScopeCommit {
		t.Fatalf("reopened in %v, want the commit scope", got)
	}

	m.drainCmds(t, m.closeDiff())
	openReviewOn(t, m, "other", gitRepoWithCommitStack(t))
	if got := m.review.Scope(); got != git.ScopeUncommitted {
		t.Fatalf("another session opened in %v, want uncommitted", got)
	}
}
