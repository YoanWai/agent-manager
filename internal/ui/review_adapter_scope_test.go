package ui

import (
	"slices"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
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

// A session with no scope of its own opens in the one Settings picks, and
// one with a scope stored keeps it. The row offers the scopes review stores.
func TestReviewOpensInTheDefaultScope(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.ReviewScopeSetting, "commit"); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "stack", gitRepoWithCommitStack(t))
	if got := m.review.Scope(); got != git.ScopeCommit {
		t.Fatalf("opened in %v, want the commit scope Settings picks", got)
	}
	sess, _ := m.diffSession()
	if err := m.services.store.SetReviewScope(sess.ID, "staged"); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.closeDiff())
	m.selectSessionRow(t, "stack")
	m.drainCmds(t, m.openDiff())
	if got := m.review.Scope(); got != git.ScopeStaged {
		t.Fatalf("opened in %v, want the session's own staged", got)
	}

	var names []string
	for scope := git.ScopeUncommitted; ; {
		names = append(names, uireview.ScopeName(scope))
		if scope = scope.Next(); scope == git.ScopeUncommitted {
			break
		}
	}
	if !slices.Equal(names, uisettings.ReviewScopes) {
		t.Fatalf("review stores %v, Settings offers %v", names, uisettings.ReviewScopes)
	}
}
