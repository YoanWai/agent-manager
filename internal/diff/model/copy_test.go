package model_test

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff/model"
	"github.com/YoanWai/agent-manager/internal/git/value"
)

func TestCloneDoesNotRetainSourceLinesOrCachedRows(t *testing.T) {
	file := model.BuildFile([]byte("old\n"), []byte("new\n"), value.ChangedFile{Path: "sample"}, value.FileStat{})
	file.SideBySideRows()
	copied := file.Clone()
	copied.Lines[0].Text = "changed"
	copied.SideBySideRows()[0].Left = 99
	if file.Lines[0].Text != "old" || file.SideBySideRows()[0].Left != 0 {
		t.Fatal("clone retained source lines or cached rows")
	}
}

func TestCloneCopiesNestedSpansAndChangeIndices(t *testing.T) {
	set := model.Set{Files: []model.FileDiff{{Lines: []model.Line{{Spans: []model.Span{{Start: 1, End: 2}}}}, Changes: []int{3}}}}
	copied := set.Clone()
	copied.Files[0].Lines[0].Spans[0].Start = 9
	copied.Files[0].Changes[0] = 7
	if got := set.Files[0]; got.Lines[0].Spans[0].Start != 1 || got.Changes[0] != 3 {
		t.Fatalf("nested clone retained source storage: %+v", got)
	}
}
