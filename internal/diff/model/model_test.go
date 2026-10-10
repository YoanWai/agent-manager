package model_test

import (
	"github.com/YoanWai/agent-manager/internal/diff/model"
	"github.com/YoanWai/agent-manager/internal/git/value"
	"testing"
)

func TestPureLineModelPairsChangedRows(t *testing.T) {
	file := model.BuildFile([]byte("before\n"), []byte("after\n"), value.ChangedFile{Path: "a.txt", Status: value.Modified}, value.FileStat{Adds: 1, Dels: 1})
	if !file.Loaded() {
		t.Fatal("built content is not loaded")
	}
	rows := file.SideBySideRows()
	if len(rows) != 1 || rows[0].Left != 0 || rows[0].Right != 1 {
		t.Fatalf("paired rows: %#v", rows)
	}
}

func TestScopeCyclePreservesStoredNames(t *testing.T) {
	scope := value.ScopeUncommitted
	for _, want := range []string{"uncommitted", "vs target", "last commit", "staged"} {
		if scope.String() != want {
			t.Fatalf("scope %d: %q, want %q", scope, scope.String(), want)
		}
		scope = scope.Next()
	}
	if scope != value.ScopeUncommitted {
		t.Fatal("scope cycle did not wrap")
	}
}
