package review_test

import (
	"errors"
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func loadedReview(t *testing.T) (*review.Model, review.LoadRequest) {
	t.Helper()
	model := review.New(false)
	req := model.Open(review.Target{ID: "s1", Name: "agent", Tool: "codex", Cwd: "/repo"}, git.ScopeUncommitted, "/repo")
	fd := diff.BuildFile(nil, []byte("new line\n"), git.ChangedFile{Path: "a.go"}, git.FileStat{})
	result := model.ApplyLoad(review.LoadResult{
		TargetID: "s1", Scope: req.Scope, Generation: req.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"},
		Set: diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{fd}},
	})
	if !result.Accepted {
		t.Fatal("fixture load rejected")
	}
	return &model, req
}

func TestDraftOwnsKeyPriorityAndSurvivesDeferredLoad(t *testing.T) {
	model, req := loadedReview(t)
	if !model.OpenAnnotation() {
		t.Fatal("annotation did not open")
	}
	model.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("keep this")}, review.KeyContext{CodeHeight: 10})

	replacement := diff.BuildFile(nil, []byte("replacement\n"), git.ChangedFile{Path: "other.go"}, git.FileStat{})
	got := model.ApplyLoad(review.LoadResult{
		TargetID: "s1", Scope: req.Scope, Generation: req.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"},
		Set: diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{replacement}},
	})
	if !got.Accepted {
		t.Fatal("current load should be consumed while draft is open")
	}
	if file, _ := model.CurrentFile(); file.File.Path != "a.go" {
		t.Fatalf("load moved draft to %q", file.File.Path)
	}
	if !model.Snapshot().Annotating {
		t.Fatal("load closed annotation")
	}

	model.Key(tea.KeyMsg{Type: tea.KeyEnter}, review.KeyContext{CodeHeight: 10})
	state := model.SavedState()
	if len(state.Comments) != 1 || state.Comments[0].Text != "keep this" {
		t.Fatalf("saved comments = %#v", state.Comments)
	}
}

func TestSendFailureRestoresDraftRound(t *testing.T) {
	model, _ := loadedReview(t)
	model.OpenAnnotation()
	model.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix it")}, review.KeyContext{CodeHeight: 10})
	model.Key(tea.KeyMsg{Type: tea.KeyEnter}, review.KeyContext{CodeHeight: 10})

	sending := model.BeginSend()
	if sending.Requests.Send == nil {
		t.Fatalf("send request missing: %s", sending.Error)
	}
	req := sending.Requests.Send
	if got := model.DraftCount(); got != 0 {
		t.Fatalf("drafts during send = %d, want 0", got)
	}
	model.ApplySend(review.SendResult{
		TargetID: "s1", RepoRoot: "/repo", CommentIDs: req.CommentIDs,
		PreviousRound: req.PreviousRound, Round: req.Round, Count: req.Count,
		TargetName: "agent", Err: errors.New("send failed"),
	})
	if got := model.DraftCount(); got != 1 {
		t.Fatalf("drafts after failed send = %d, want 1", got)
	}
	if got := model.Round().Number; got != 0 {
		t.Fatalf("round after failed send = %d, want 0", got)
	}
}
