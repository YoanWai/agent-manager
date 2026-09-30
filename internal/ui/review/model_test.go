package review_test

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/ui/review"
)

func TestCloseRevokesLoadsAndKeepsPerTargetState(t *testing.T) {
	var model review.Model
	req := model.Open(review.Target{ID: "s1", Name: "agent", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	loaded := review.LoadResult{
		TargetID: req.Target.ID, Scope: req.Scope, Generation: req.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, SavedLoaded: true,
		Saved: review.SavedState{Reviewed: map[string]uint64{"uncommitted\x00a.go": 42}},
		Set:   diff.Set{},
	}
	if got := model.ApplyLoad(loaded); !got.Accepted {
		t.Fatal("initial load was rejected")
	}
	before := model.Generation()
	model.Close()
	if model.Active() || model.Generation() != before+1 {
		t.Fatalf("close = active %v gen %d, want inactive gen %d", model.Active(), model.Generation(), before+1)
	}
	if got := model.ApplyLoad(loaded); got.Accepted {
		t.Fatal("load from the closed generation was accepted")
	}

	reopened := model.Open(review.Target{ID: "s1", Name: "agent", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	if reopened.Generation == loaded.Generation {
		t.Fatal("reopen reused the revoked generation")
	}
	model.ApplyLoad(review.LoadResult{
		TargetID: "s1", Scope: git.ScopeUncommitted, Generation: reopened.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: diff.Set{},
	})
	if got := model.SavedState().Reviewed["uncommitted\x00a.go"]; got != 42 {
		t.Fatalf("review mark after reopen = %d, want 42", got)
	}
}

func TestLoadAndStatusUseIndependentFences(t *testing.T) {
	var model review.Model
	req := model.Open(review.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	status1, ok := model.StatusRequest()
	if !ok {
		t.Fatal("status request missing")
	}
	status2, ok := model.StatusRequest()
	if !ok {
		t.Fatal("second status request missing")
	}
	if status2.Generation != status1.Generation+1 {
		t.Fatalf("status generations = %d then %d", status1.Generation, status2.Generation)
	}

	if got := model.ApplyStatus(review.StatusResult{
		TargetID: "s1", RepoRoot: "/repo", Generation: status1.Generation,
	}); got.Accepted {
		t.Fatal("stale status result was accepted")
	}
	if got := model.ApplyLoad(review.LoadResult{
		TargetID: "s1", Scope: git.ScopeUncommitted, Generation: req.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: diff.Set{},
	}); !got.Accepted {
		t.Fatal("valid load was rejected by status generation changes")
	}
	if got := model.ApplyStatus(review.StatusResult{
		TargetID: "s1", RepoRoot: "/repo", Generation: status2.Generation,
	}); !got.Accepted {
		t.Fatal("current status result was rejected")
	}
}

func TestSaveFailureIsReportedOnlyForCurrentTarget(t *testing.T) {
	var model review.Model
	req := model.Open(review.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	model.ApplyLoad(review.LoadResult{TargetID: "s1", Scope: req.Scope, Generation: req.Generation, RepoRoot: "/repo", RepoRoots: []string{"/repo"}})

	current := model.ApplySave(review.SaveResult{TargetID: "s1", RepoRoot: "/repo", Err: errors.New("disk full")})
	if current.Error == "" {
		t.Fatal("current save failure was hidden")
	}
	other := model.ApplySave(review.SaveResult{TargetID: "other", RepoRoot: "/repo", Err: errors.New("disk full")})
	if other.Error != "" {
		t.Fatalf("unrelated save surfaced %q", other.Error)
	}
}

func TestSwitchFileRestoresScrollByTargetRepoScopeAndPath(t *testing.T) {
	var model review.Model
	req := model.Open(review.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	set := diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{
		diff.BuildFile(nil, []byte("one\ntwo\nthree\n"), git.ChangedFile{Path: "a.go"}, git.FileStat{}),
		diff.BuildFile(nil, []byte("four\nfive\n"), git.ChangedFile{Path: "b.go"}, git.FileStat{}),
	}}
	model.ApplyLoad(review.LoadResult{TargetID: "s1", Scope: req.Scope, Generation: req.Generation, RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: set})
	model.MoveCursor(2, 1)
	model.SwitchFile(1)
	if got := model.Snapshot().FileIndex; got != 1 {
		t.Fatalf("file index = %d, want 1", got)
	}
	model.SwitchFile(-1)
	got := model.Snapshot()
	if got.FileIndex != 0 || got.Scroll != 2 {
		t.Fatalf("restored file/index = %d/%d, want 0/2", got.FileIndex, got.Scroll)
	}
}

func TestToggleLayoutKeepsTheSameDiffLineSelected(t *testing.T) {
	var model review.Model
	req := model.Open(review.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	fd := diff.BuildFile([]byte("old\nkeep\n"), []byte("new\nkeep\n"), git.ChangedFile{Path: "a.go"}, git.FileStat{})
	model.ApplyLoad(review.LoadResult{TargetID: "s1", Scope: req.Scope, Generation: req.Generation, RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{fd}}})
	model.SetCursorDiffLine(1, 10)
	want := model.CursorDiffLine()
	model.ToggleSideBySide(10)
	if got := model.CursorDiffLine(); got != want {
		t.Fatalf("diff line after layout toggle = %d, want %d", got, want)
	}
}

func TestApplyFileRejectsThePreviousGeneration(t *testing.T) {
	var model review.Model
	first := model.Open(review.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	unloaded := diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{{File: git.ChangedFile{Path: "a.go"}}}}
	model.ApplyLoad(review.LoadResult{TargetID: "s1", Scope: first.Scope, Generation: first.Generation, RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: unloaded})
	second := model.Open(review.Target{ID: "s1", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	model.ApplyLoad(review.LoadResult{TargetID: "s1", Scope: second.Scope, Generation: second.Generation, RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: unloaded})
	loaded := diff.BuildFile(nil, []byte("new\n"), git.ChangedFile{Path: "a.go"}, git.FileStat{})
	got := model.ApplyFile(review.FileResult{TargetID: "s1", Scope: first.Scope, Generation: first.Generation, RepoRoot: "/repo", Index: 0, Path: "a.go", File: loaded})
	if got.Accepted {
		t.Fatal("previous-generation file result was accepted")
	}
	if current, _ := model.CurrentFile(); current.Loaded() {
		t.Fatal("stale result replaced the current file")
	}
}
