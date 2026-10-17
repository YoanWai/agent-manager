package review

import (
	"strings"
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDiffCommentBoxGrowsWithText(t *testing.T) {
	m := New(false)
	request := m.Open(Target{ID: "s1"}, git.ScopeUncommitted, "/repo")
	m.ApplyLoad(LoadResult{
		TargetID: "s1", Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"},
		Set: diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{diff.BuildFile(nil, []byte("line\n"), git.ChangedFile{Path: "main.go"}, git.FileStat{})}},
	})
	m.OpenAnnotation()
	if m.AnnotationInputHeight(40) != 1 {
		t.Fatal("empty comment should stay one row")
	}
	m.AnnotationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("word ", 40))})
	got := m.AnnotationInputHeight(40)
	if got <= 1 {
		t.Fatalf("long comment stayed %d rows", got)
	}
	if got > annotationInputMaxRows {
		t.Fatalf("comment box grew past the cap: %d", got)
	}
}
