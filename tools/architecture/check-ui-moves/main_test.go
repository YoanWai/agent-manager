package main

import "testing"

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
