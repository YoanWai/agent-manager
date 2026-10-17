package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareSnapshotsAcceptsCallInitializerReordering(t *testing.T) {
	base := snapshot{CallInitializers: []string{"first", "second"}}
	target := snapshot{CallInitializers: []string{"second", "first"}}
	if err := compareSnapshots(base, target); err != nil {
		t.Fatalf("reordered unchanged initializer set: %v", err)
	}
}

func TestCompareSnapshotsRejectsDeclarationChange(t *testing.T) {
	base := snapshot{Declarations: []declaration{{Key: "kept", Hash: "before"}}}
	target := snapshot{Declarations: []declaration{{Key: "kept", Hash: "after"}}}
	if err := compareSnapshots(base, target); err == nil {
		t.Fatal("changed declaration passed")
	}
}

func TestCompareSnapshotsRejectsStandaloneCommentLoss(t *testing.T) {
	base := snapshot{CommentTokens: []count{{Text: "// reason", Count: 1}}}
	if err := compareSnapshots(base, snapshot{}); err == nil {
		t.Fatal("lost standalone comment passed")
	}
}

func TestCompareSnapshotsRejectsInitFunctionReordering(t *testing.T) {
	base := snapshot{InitFunctions: []string{"first", "second"}}
	target := snapshot{InitFunctions: []string{"second", "first"}}
	if err := compareSnapshots(base, target); err == nil {
		t.Fatal("reordered init functions passed")
	}
}

func TestLoadWorktreeUsesSelectedPackageRoot(t *testing.T) {
	previous := sourceRoot
	t.Cleanup(func() { sourceRoot = previous })
	sourceRoot = t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "kept.go"), []byte("package selected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "ignored.txt"), []byte("not Go"), 0600); err != nil {
		t.Fatal(err)
	}
	files := loadFiles("WORKTREE", nil)
	if len(files) != 1 || filepath.Base(files[0].Path) != "kept.go" {
		t.Fatalf("wrong package files: %+v", files)
	}
	if excluded := loadFiles("WORKTREE", map[string]bool{"kept.go": true}); len(excluded) != 0 {
		t.Fatalf("excluded files: %+v", excluded)
	}
}
