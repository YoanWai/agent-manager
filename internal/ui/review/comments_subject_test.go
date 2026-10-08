package review

import (
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
)

func TestReviewSubjectNamesTheCheckout(t *testing.T) {
	const dir, head, mergeBase = "/work/repo", "a1b2c3d", "e4f5a6b"
	onBranch := diff.Set{Repo: git.Repo{Root: dir, Branch: "stack-2", Head: head}, BaseDesc: "stack-1@" + mergeBase}
	for _, tc := range []struct {
		scope git.Scope
		want  string
	}{
		{git.ScopeBranch, "the branch changes on `stack-2` (`" + head + "`) vs `stack-1` (merge-base `" + mergeBase + "`) in `" + dir + "`"},
		{git.ScopeLastCommit, "the last commit on `stack-2` (`" + head + "`) in `" + dir + "`"},
		{git.ScopeStaged, "the staged changes on `stack-2` (`" + head + "`) in `" + dir + "`"},
		{git.ScopeUncommitted, "the uncommitted changes on `stack-2` (`" + head + "`) in `" + dir + "`"},
	} {
		if got := reviewSubject(tc.scope, onBranch, dir); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.scope, got, tc.want)
		}
	}

	detached := diff.Set{Repo: git.Repo{Root: dir, Head: head, Detached: true}}
	if got, want := reviewSubject(git.ScopeUncommitted, detached, dir), "the uncommitted changes on detached HEAD `"+head+"` in `"+dir+"`"; got != want {
		t.Errorf("detached:\n got %s\nwant %s", got, want)
	}

	if got, want := reviewSubject(git.ScopeBranch, diff.Set{}, dir), "the branch changes in `"+dir+"`"; got != want {
		t.Errorf("a diff that failed to load:\n got %s\nwant %s", got, want)
	}

	if got, want := reviewSubject(git.ScopeUncommitted, diff.Set{}, "/tmp/repo\ntwo\x1b"), "the uncommitted changes in `/tmp/repo two^[`"; got != want {
		t.Errorf("a folder name with control bytes:\n got %q\nwant %q", got, want)
	}
}
