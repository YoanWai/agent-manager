package review_test

import (
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/review"
)

func TestLoadInputAndReadViewsDoNotAliasOwnedContent(t *testing.T) {
	model := review.New(false)
	target := review.Target{ID: "session"}
	request := model.Open(target, git.ScopeUncommitted, "/repo")
	set := diff.Set{
		Repo: git.Repo{Root: "/repo"},
		Files: []diff.FileDiff{diff.BuildFile(
			[]byte("old\n"), []byte("new\n"), git.ChangedFile{Path: "main.go"}, git.FileStat{},
		)},
	}
	model.ApplyLoad(review.LoadResult{
		TargetID: target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: set,
	})

	set.Files[0].File.Path = "input-mutated.go"
	set.Files[0].Lines[0].Text = "input mutated"
	current, ok := model.CurrentFile()
	if !ok || current.File.Path != "main.go" || current.Lines[0].Text == "input mutated" {
		t.Fatalf("load input aliases owned content: %+v", current)
	}

	current.File.Path = "output-mutated.go"
	current.Lines[0].Text = "output mutated"
	setCopy := model.SetCopy()
	setCopy.Files[0].Lines[0].Spans = append(setCopy.Files[0].Lines[0].Spans, diff.Span{Start: 1, End: 2})
	summary := model.Snapshot()
	summary.Set.Files[0].File.Path = "summary-mutated.go"

	current, _ = model.CurrentFile()
	if current.File.Path != "main.go" || current.Lines[0].Text == "output mutated" || len(current.Lines[0].Spans) != 0 {
		t.Fatalf("read view aliases owned content: %+v", current)
	}
	if got := model.Snapshot().Set.Files[0].File.Path; got != "main.go" {
		t.Fatalf("summary aliases owned content: %q", got)
	}
}

func TestFileResultDoesNotAliasOwnedContent(t *testing.T) {
	model := review.New(false)
	target := review.Target{ID: "session"}
	request := model.Open(target, git.ScopeUncommitted, "/repo")
	set := diff.Set{
		Repo:  git.Repo{Root: "/repo"},
		Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}},
	}
	result := model.ApplyLoad(review.LoadResult{
		TargetID: target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: set,
	})
	if len(result.Requests.Files) != 1 {
		t.Fatalf("file requests = %d, want 1", len(result.Requests.Files))
	}
	loaded := diff.BuildFile([]byte("old\n"), []byte("new\n"), git.ChangedFile{Path: "main.go"}, git.FileStat{})
	fileRequest := result.Requests.Files[0]
	model.ApplyFile(review.FileResult{
		TargetID: target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/repo", Index: fileRequest.Index, Path: fileRequest.Path, File: loaded,
	})
	loaded.Lines[0].Text = "mutated"
	current, _ := model.CurrentFile()
	if current.Lines[0].Text == "mutated" {
		t.Fatal("file result aliases owned content")
	}
}

func TestHighlightLineFallsBackWhenNoPreparedTextExists(t *testing.T) {
	highlight := review.NewHighlight([]string{""})
	if got := highlight.Line(0, "plain text"); got != "plain text" {
		t.Fatalf("line = %q, want fallback", got)
	}
}
