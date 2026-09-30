package ui

import (
	"github.com/YoanWai/agent-manager/internal/git"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReviewedMarkClearsOnContentChange(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "reset", dir)
	if len(m.diff.set.Files) == 0 {
		t.Fatal("want at least one changed file")
	}
	path := m.diff.set.Files[0].File.Path

	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed(path) {
		t.Fatal("file should be reviewed after toggle")
	}

	original, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	changed := string(original) + "\nfunc Added() {}"
	if err := os.WriteFile(filepath.Join(dir, path), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	m.refreshDiff(t)
	if m.fileReviewed(path) {
		t.Fatal("reviewed mark should reset after content changes")
	}
}

// A scope that does not list a file says nothing about whether it changed,
// so its mark waits for the scope that shows it again.
func TestAScopeMissingAFileKeepsItsReviewedMark(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scopemarks", gitTestRepo(t))
	fd := m.currentFileDiff()
	if fd == nil {
		t.Fatal("review has no selected file")
	}
	path := fd.File.Path
	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed(path) {
		t.Fatalf("%s was not marked reviewed", path)
	}

	m.diff.set.Files = nil
	if clearStaleReviewedMarks(m) {
		t.Fatal("a file the scope does not list counted as a stale mark")
	}
	if !m.fileReviewed(path) {
		t.Fatalf("the mark for %s did not survive a scope without it", path)
	}
}

// One trip around the scope cycle must not destroy a saved reviewed mark:
// the same file renders different diff lines - so a different content hash -
// under another scope, and only the scope a mark was taken in may judge it
// stale.
func TestScopeCycleKeepsReviewedMark(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitTestRepo(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Stage the current edit and add another on top, so the staged and
	// uncommitted scopes both list main.go with different rendered diffs.
	runGit("add", "main.go")
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() { println(2) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "scopecycle", dir)
	target := -1
	for i, fd := range m.diff.set.Files {
		if fd.File.Path == "main.go" {
			target = i
		}
	}
	if target < 0 {
		t.Fatalf("main.go missing from the uncommitted scope: %+v", m.diff.set.Files)
	}
	m.drainCmds(t, m.switchDiffFile(target-m.diff.fileIdx))
	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed("main.go") {
		t.Fatal("main.go was not marked reviewed")
	}

	for cycle := 0; cycle < 4; cycle++ {
		m.drainCmds(t, m.cycleDiffScope())
		if m.diff.scope != git.ScopeUncommitted && m.fileReviewed("main.go") {
			t.Fatalf("a mark taken under uncommitted read as reviewed under %q", m.diff.scope)
		}
	}
	if m.diff.scope != git.ScopeUncommitted {
		t.Fatalf("four cycles should return to uncommitted, got %q", m.diff.scope)
	}
	if !m.fileReviewed("main.go") {
		t.Fatal("cycling scopes destroyed the reviewed mark")
	}
}
