package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRepo(t *testing.T) (*Driver, string) {
	t.Helper()
	driver, err := New()
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@test")
	gitIn(t, dir, "config", "user.name", "test")
	return driver, dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", message)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNotARepo(t *testing.T) {
	driver, err := New()
	if err != nil {
		t.Skip("git not installed")
	}
	if _, err := driver.OpenRepo(t.TempDir()); err != ErrNotARepo {
		t.Fatalf("want ErrNotARepo, got %v", err)
	}
}

func initRepoAt(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestResolveReposSingle(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "init")
	roots, err := driver.ResolveRepos(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("want 1 root, got %v", roots)
	}
}

func TestResolveReposUmbrellaRanksDirtyFirst(t *testing.T) {
	driver, err := New()
	if err != nil {
		t.Skip("git not installed")
	}
	umbrella := t.TempDir()
	clean := filepath.Join(umbrella, "clean")
	dirty := filepath.Join(umbrella, "dirty")
	initRepoAt(t, clean)
	write(t, clean, "a.go", "package a\n")
	commit(t, clean, "init")
	initRepoAt(t, dirty)
	write(t, dirty, "a.go", "package a\n")
	commit(t, dirty, "init")
	write(t, dirty, "b.go", "package a\n")

	roots, err := driver.ResolveRepos(umbrella)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("want 2 roots, got %v", roots)
	}
	if filepath.Base(roots[0]) != "dirty" {
		t.Fatalf("want dirty repo first, got %v", roots)
	}
}

func TestResolveReposNone(t *testing.T) {
	driver, err := New()
	if err != nil {
		t.Skip("git not installed")
	}
	if _, err := driver.ResolveRepos(t.TempDir()); err != ErrNotARepo {
		t.Fatalf("want ErrNotARepo, got %v", err)
	}
}

func TestUncommittedScope(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	commit(t, dir, "init")
	write(t, dir, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	write(t, dir, "new.txt", "hello\n")

	repo, err := driver.OpenRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Branch != "main" || repo.Unborn {
		t.Fatalf("repo = %+v", repo)
	}
	files, err := driver.ChangedFiles(repo.Root, ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	byPath := map[string]Status{}
	for _, f := range files {
		byPath[f.Path] = f.Status
	}
	if byPath["a.go"] != Modified || byPath["new.txt"] != Untracked {
		t.Fatalf("statuses = %v", byPath)
	}

	old, err := driver.ShowFile(repo.Root, "HEAD", "a.go")
	if err != nil || string(old) != "package a\n\nfunc A() {}\n" {
		t.Fatalf("ShowFile = %q, %v", old, err)
	}
	work, err := driver.WorkingFile(repo.Root, "a.go")
	if err != nil || string(work) == string(old) {
		t.Fatalf("WorkingFile = %q, %v", work, err)
	}
	missing, err := driver.ShowFile(repo.Root, "HEAD", "new.txt")
	if err != nil || missing != nil {
		t.Fatalf("absent path should be empty, got %q, %v", missing, err)
	}
}

func TestRenameAndNumStat(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "old.txt", "line1\nline2\nline3\nline4\nline5\n")
	commit(t, dir, "init")
	if err := os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	commit(t, dir, "rename")

	files, err := driver.ChangedFiles(dir, ScopeLastCommit, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != Renamed || files[0].OldPath != "old.txt" || files[0].Path != "renamed.txt" {
		t.Fatalf("files = %+v", files)
	}
	stats, err := driver.NumStat(dir, ScopeLastCommit, "")
	if err != nil {
		t.Fatal(err)
	}
	if stat, ok := stats["renamed.txt"]; !ok || stat.Adds != 0 || stat.Dels != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestSingleCommitLastScope(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "one\n")
	commit(t, dir, "only")
	files, err := driver.ChangedFiles(dir, ScopeLastCommit, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != Added {
		t.Fatalf("files = %+v", files)
	}
}

func TestStagedScope(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "one\n")
	commit(t, dir, "init")
	write(t, dir, "a.txt", "one\ntwo\n")
	cmd := exec.Command("git", "add", "a.txt")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "one\ntwo\nthree\n")

	files, err := driver.ChangedFiles(dir, ScopeStaged, "")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %+v, %v", files, err)
	}
	staged, err := driver.IndexFile(dir, "a.txt")
	if err != nil || string(staged) != "one\ntwo\n" {
		t.Fatalf("IndexFile = %q, %v", staged, err)
	}
}

func TestBranchScope(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "base\n")
	commit(t, dir, "base")
	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "b.txt", "feature\n")
	commit(t, dir, "feature work")

	base, describe, err := driver.BaseRef(dir)
	if err != nil || base == "" || describe == "" {
		t.Fatalf("BaseRef = %q, %q, %v", base, describe, err)
	}
	files, err := driver.ChangedFiles(dir, ScopeBranch, base)
	if err != nil || len(files) != 1 || files[0].Path != "b.txt" {
		t.Fatalf("files = %+v, %v", files, err)
	}
}

func TestFingerprintChanges(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "one\n")
	commit(t, dir, "init")
	before, err := driver.Fingerprint(dir, ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "changed\n")
	after, err := driver.Fingerprint(dir, ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("fingerprint should change with the working tree")
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("plain text\n")) {
		t.Fatal("text flagged binary")
	}
	if !IsBinary([]byte{'a', 0, 'b'}) {
		t.Fatal("NUL not flagged binary")
	}
}

func TestChangedFilesSkipsNestedRepoDirectories(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "untracked.go", "package a\n\nfunc B() {}\n")
	initRepoAt(t, filepath.Join(dir, "nested"))

	files, err := driver.ChangedFiles(dir, ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Path, "/") {
			t.Fatalf("directory entry leaked into the file list: %q", file.Path)
		}
		if file.Path == "nested" {
			t.Fatal("nested repository should not be listed")
		}
	}
	found := false
	for _, file := range files {
		if file.Path == "untracked.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("untracked file should still be listed, got %+v", files)
	}
}

func TestCountWorkingLines(t *testing.T) {
	driver, dir := testRepo(t)
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"trailing newline", "a\nb\nc\n", 3},
		{"no trailing newline", "a\nb\nc", 3},
		{"one line", "only\n", 1},
		{"blank lines counted", "\n\n\n", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := strings.ReplaceAll(tc.name, " ", "_") + ".txt"
			if err := os.WriteFile(filepath.Join(dir, path), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			count, err := driver.CountWorkingLines(dir, path)
			if err != nil {
				t.Fatal(err)
			}
			if !count.Counted || count.Binary {
				t.Fatalf("count = %+v, want a plain counted result", count)
			}
			if count.Lines != tc.want {
				t.Errorf("lines = %d, want %d", count.Lines, tc.want)
			}
		})
	}
}

// A count that spans more than one read buffer must still be right.
func TestCountWorkingLinesAcrossBuffers(t *testing.T) {
	driver, dir := testRepo(t)
	var b strings.Builder
	const want = 40000
	for i := 0; i < want; i++ {
		b.WriteString("some line of text\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := driver.CountWorkingLines(dir, "long.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !count.Counted || count.Lines != want {
		t.Fatalf("count = %+v, want %d counted lines", count, want)
	}
}

func TestCountWorkingLinesBinary(t *testing.T) {
	driver, dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := driver.CountWorkingLines(dir, "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if !count.Binary {
		t.Fatalf("count = %+v, want binary", count)
	}
	if count.Lines != 0 {
		t.Errorf("binary file got a line count of %d", count.Lines)
	}
}

// Past the scan budget the count is reported unknown, never as zero.
func TestCountWorkingLinesTooLarge(t *testing.T) {
	driver, dir := testRepo(t)
	path := filepath.Join(dir, "huge.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCountBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()

	count, err := driver.CountWorkingLines(dir, "huge.bin")
	if err != nil {
		t.Fatal(err)
	}
	if count.Counted {
		t.Fatalf("count = %+v, want an uncounted result past the budget", count)
	}
	if count.Lines != 0 {
		t.Errorf("uncounted result carried %d lines", count.Lines)
	}
}

func TestCountWorkingLinesMissingFileIsUncounted(t *testing.T) {
	driver, dir := testRepo(t)
	count, err := driver.CountWorkingLines(dir, "gone.txt")
	if err != nil {
		t.Fatal(err)
	}
	if count.Counted {
		t.Fatalf("count = %+v, want uncounted for a vanished file", count)
	}
}

func TestWorktreesListsBranches(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "init")
	wtDir := filepath.Join(t.TempDir(), "wt-feature")
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("worktree", "add", "-b", "feature/x", wtDir)

	worktrees, err := driver.Worktrees(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("want 2 worktrees, got %+v", worktrees)
	}
	byBranch := map[string]string{}
	for _, wt := range worktrees {
		byBranch[wt.Branch] = wt.Root
	}
	if _, ok := byBranch["feature/x"]; !ok {
		t.Fatalf("feature/x missing: %+v", worktrees)
	}
	if _, ok := byBranch["main"]; !ok {
		t.Fatalf("main missing: %+v", worktrees)
	}
}

func TestIsRepoRoot(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "init")
	if !driver.IsRepoRoot(dir) {
		t.Fatal("repo root should be recognised")
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if driver.IsRepoRoot(sub) {
		t.Fatal("a subdirectory is not the root")
	}
	if driver.IsRepoRoot(t.TempDir()) {
		t.Fatal("a non-repo dir is not a root")
	}
}

func TestBranchRefsExcludesOriginHead(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "base\n")
	commit(t, dir, "base")
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("checkout", "-b", "feature")
	// A remote-tracking origin/HEAD and origin ref must be filtered out.
	runGit("update-ref", "refs/remotes/origin/main", "main")
	runGit("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	refs, err := driver.BranchRefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ref := range refs {
		got[ref] = true
	}
	for _, want := range []string{"main", "feature", "origin/main"} {
		if !got[want] {
			t.Fatalf("BranchRefs missing %q: %v", want, refs)
		}
	}
	for _, unwanted := range []string{"origin", "origin/HEAD"} {
		if got[unwanted] {
			t.Fatalf("BranchRefs must exclude %q: %v", unwanted, refs)
		}
	}
}

func TestResolveRef(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "base\n")
	commit(t, dir, "base")
	if err := driver.ResolveRef(dir, "main"); err != nil {
		t.Fatalf("main should resolve: %v", err)
	}
	if err := driver.ResolveRef(dir, "nope"); err == nil {
		t.Fatal("an unknown ref must fail")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error should name the ref, got %v", err)
	}
}

func TestResolveReposIncludesOutsideWorktrees(t *testing.T) {
	driver, err := New()
	if err != nil {
		t.Skip("git not installed")
	}
	umbrella := t.TempDir()
	repo := filepath.Join(umbrella, "alpha")
	initRepoAt(t, repo)
	write(t, repo, "a.go", "package a\n")
	commit(t, repo, "init")

	outside := filepath.Join(t.TempDir(), "wt-hot")
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("worktree", "add", "-b", "feature/hot", outside)
	write(t, outside, "b.go", "package a\n\nfunc B() {}\n")

	roots, err := driver.ResolveRepos(umbrella)
	if err != nil {
		t.Fatal(err)
	}
	resolvedOutside, _ := filepath.EvalSymlinks(outside)
	first, _ := filepath.EvalSymlinks(roots[0])
	if first != resolvedOutside {
		t.Fatalf("dirty outside worktree should rank first, got %v", roots)
	}
	seen := map[string]int{}
	for _, root := range roots {
		resolved, _ := filepath.EvalSymlinks(root)
		seen[resolved]++
	}
	if len(roots) != 2 || seen[resolvedOutside] != 1 {
		t.Fatalf("want repo + worktree once each, got %v", roots)
	}
}

func TestResolveReposSingleIncludesWorktrees(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "init")

	worktree := filepath.Join(t.TempDir(), "wt-feature")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/x", worktree)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}

	roots, err := driver.ResolveRepos(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("want repo + worktree, got %v", roots)
	}
	resolvedRepo, _ := filepath.EvalSymlinks(dir)
	first, _ := filepath.EvalSymlinks(roots[0])
	if first != resolvedRepo {
		t.Fatalf("cwd repo should stay first, got %v", roots)
	}
}

func TestRepoRoot(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	root, err := driver.RepoRoot(dir)
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if resolved, _ := filepath.EvalSymlinks(dir); root != resolved && root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
	if _, err := driver.RepoRoot(t.TempDir()); err == nil {
		t.Fatal("non-repo dir should error")
	}
}

func TestAddWorktree(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")

	path, branch, err := driver.AddWorktree(dir, "my feat/1")
	if err != nil {
		t.Fatalf("add worktree: %v", err)
	}
	wantPath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "my-feat-1")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	if branch != "am/my-feat-1" {
		t.Fatalf("branch = %q", branch)
	}
	if _, err := os.Stat(filepath.Join(path, "a.txt")); err != nil {
		t.Fatalf("worktree missing checkout: %v", err)
	}

	if _, _, err := driver.AddWorktree(dir, "my feat/1"); err == nil {
		t.Fatal("existing path should error")
	}
}

func TestAddWorktreeEmptyRepoFails(t *testing.T) {
	driver, dir := testRepo(t)
	if _, _, err := driver.AddWorktree(dir, "feat"); err == nil {
		t.Fatal("repo with no commits has no base ref, want error")
	}
}

func TestRemoveWorktreeIfClean(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "clean")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	removed, err := driver.RemoveWorktreeIfClean(dir, path, branch)
	if err != nil || !removed {
		t.Fatalf("clean worktree should remove: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("worktree directory still on disk")
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("branch should be deleted")
	}
}

func TestRenameWorktreeBranchKeepsDirectory(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	write(t, path, "wip.txt", "uncommitted work")

	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "release the version")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if newBranch != "am/release-the-version" {
		t.Fatalf("branch = %q", newBranch)
	}
	if _, err := os.Stat(filepath.Join(path, "wip.txt")); err != nil {
		t.Fatalf("worktree lost uncommitted work: %v", err)
	}
	moved := filepath.Join(filepath.Dir(path), "release-the-version")
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatalf("rename created a new worktree directory: %v", err)
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("old branch should be gone")
	}
	head, err := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || head != newBranch {
		t.Fatalf("worktree HEAD = %q err=%v, want %q", head, err, newBranch)
	}
	if removed, err := driver.RemoveWorktreeIfClean(dir, path, newBranch); err != nil || removed {
		t.Fatalf("renamed worktree still holds work: removed=%v err=%v", removed, err)
	}
}

func TestRenameWorktreeBranchSameNameKeepsEverything(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "steady")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Both names sanitize to "steady", so there is nothing to rename.
	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "steady/")
	if err != nil {
		t.Fatalf("same name should be a no-op: %v", err)
	}
	if newBranch != branch {
		t.Fatalf("no-op returned %q, want %q", newBranch, branch)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree directory should survive a no-op: %v", err)
	}
}

func TestRenameWorktreeBranchRefusesTakenBranch(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "mover")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := driver.AddWorktree(dir, "taken"); err != nil {
		t.Fatalf("add taken: %v", err)
	}

	if _, err := driver.RenameWorktreeBranch(dir, path, branch, "taken"); err == nil {
		t.Fatal("an occupied branch should fail the rename")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("refused rename should leave the worktree in place: %v", err)
	}
	if head, err := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD"); err != nil || head != branch {
		t.Fatalf("refused rename changed the branch: %q err=%v", head, err)
	}

	// A free directory whose branch is already taken is refused just the same.
	if _, err := driver.run(dir, "branch", "am/reserved"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if _, err := driver.RenameWorktreeBranch(dir, path, branch, "reserved"); err == nil {
		t.Fatal("an occupied branch should fail the rename")
	}
}

func TestRenameWorktreeBranchIgnoresTakenDirectory(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "mover")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	taken := filepath.Join(filepath.Dir(path), "taken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "taken")
	if err != nil {
		t.Fatalf("directory name should not block the branch rename: %v", err)
	}
	if newBranch != "am/taken" {
		t.Fatalf("branch = %q", newBranch)
	}
	if _, err := os.Stat(taken); err != nil {
		t.Fatalf("existing directory was disturbed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree directory moved: %v", err)
	}
}

func TestRenameWorktreeBranchLeavesUnrecognizedWorktreeAlone(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "handed-over")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// The branch was renamed by hand, so the worktree is no longer the
	// manager's to rename.
	if _, err := driver.run(dir, "branch", "-m", branch, "feat/mine"); err != nil {
		t.Fatalf("rename branch: %v", err)
	}
	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "renamed")
	if err != nil {
		t.Fatalf("a branch git no longer has should be left alone: %v", err)
	}
	if newBranch != branch {
		t.Fatalf("returned %q, want the recorded %q", newBranch, branch)
	}
	if head, _ := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD"); head != "feat/mine" {
		t.Fatalf("hand-renamed branch was disturbed: %q", head)
	}

	gone := filepath.Join(filepath.Dir(dir), "nowhere")
	if b, err := driver.RenameWorktreeBranch(dir, gone, branch, "renamed"); err != nil || b != branch {
		t.Fatalf("a missing directory should be left alone: %q %v", b, err)
	}
}

func TestRenameWorktreeBranchLeavesSwitchedWorktreeAlone(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "managed")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := driver.run(path, "switch", "-c", "feat/mine"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	got, err := driver.RenameWorktreeBranch(dir, path, branch, "renamed")
	if err != nil {
		t.Fatalf("a worktree switched to another branch is left alone, not refused: %v", err)
	}
	if got != branch {
		t.Fatalf("returned %q, want the recorded %q", got, branch)
	}
	if head, err := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD"); err != nil || head != "feat/mine" {
		t.Fatalf("switched branch was disturbed: %q err=%v", head, err)
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		t.Fatalf("recorded branch was disturbed: %v", err)
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/am/renamed"); err == nil {
		t.Fatal("a left-alone worktree must not gain the destination branch")
	}
}

func TestRenameWorktreeBranchLeavesDetachedWorktreeAlone(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "managed")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := driver.run(path, "checkout", "--detach"); err != nil {
		t.Fatalf("detach: %v", err)
	}

	got, err := driver.RenameWorktreeBranch(dir, path, branch, "renamed")
	if err != nil {
		t.Fatalf("a detached worktree is left alone, not refused: %v", err)
	}
	if got != branch {
		t.Fatalf("returned %q, want the recorded %q", got, branch)
	}
	if head, err := driver.run(path, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		t.Fatalf("the worktree was put back on branch %q instead of left detached", head)
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		t.Fatalf("recorded branch was disturbed: %v", err)
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/am/renamed"); err == nil {
		t.Fatal("a left-alone worktree must not gain the destination branch")
	}
}

func TestRenameWorktreeBranchPropagatesGitErrors(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	notRepo := t.TempDir()
	if _, err := driver.RenameWorktreeBranch(notRepo, path, branch, "renamed"); err == nil {
		t.Fatal("a root that is not a repo should fail the rename, not pass as a missing branch")
	}
	head, err := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || head != branch {
		t.Fatalf("failed rename disturbed HEAD: %q err=%v", head, err)
	}
}

func TestRenameWorktreeBranchUnreadableDirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads through a stripped directory")
	}
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "unreadable")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// A worktree the manager cannot even look at is not a worktree that
	// has been removed, so it must not pass for one.
	parent := filepath.Dir(path)
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o755) })

	if _, err := driver.RenameWorktreeBranch(dir, path, branch, "renamed"); err == nil {
		t.Fatal("a directory that cannot be read should fail the rename, not pass as removed")
	}
}

func TestRenameWorktreeBranchEmptyNameFails(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "named")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := driver.RenameWorktreeBranch(dir, path, branch, "///"); err == nil {
		t.Fatal("a name with nothing usable should fail")
	}
}

func TestRenameWorktreeBranchKeepsLiveProcessCwd(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	tmp := t.TempDir()
	goFile := filepath.Join(tmp, "go")
	pwdFile := filepath.Join(tmp, "pwd")
	cmd := exec.Command("sh", "-c", `while [ ! -f "$1" ]; do sleep 0.05; done
echo still-here > live.txt
pwd > "$2"`, "worktree-proc", goFile, pwdFile)
	cmd.Dir = path
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "release the version")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if newBranch != "am/release-the-version" {
		t.Fatalf("branch = %q", newBranch)
	}
	if err := os.WriteFile(goFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("release process: %v", err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("process: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process did not finish after the directory was supposed to stay put")
	}

	if _, err := os.Stat(filepath.Join(path, "live.txt")); err != nil {
		t.Fatalf("spawn-time path lost the process write: %v", err)
	}
	got, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("read pwd: %v", err)
	}
	if !sameResolvedPath(t, strings.TrimSpace(string(got)), path) {
		t.Fatalf("live process cwd = %q, want spawn path %q", strings.TrimSpace(string(got)), path)
	}
	head, err := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || head != newBranch {
		t.Fatalf("spawn-time path HEAD = %q err=%v, want %q", head, err, newBranch)
	}
}

func TestRenameWorktreeBranchTwiceKeepsDirectory(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	first, err := driver.RenameWorktreeBranch(dir, path, branch, "first pass")
	if err != nil {
		t.Fatalf("first rename: %v", err)
	}
	second, err := driver.RenameWorktreeBranch(dir, path, first, "second pass")
	if err != nil {
		t.Fatalf("second rename: %v", err)
	}
	if second != "am/second-pass" {
		t.Fatalf("branch = %q", second)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("spawn-time directory moved: %v", err)
	}
	head, err := driver.run(path, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || head != second {
		t.Fatalf("HEAD = %q err=%v, want %q", head, err, second)
	}
}

func TestRenameWorktreeBranchLetsAgentKeepWorking(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	tmp := t.TempDir()
	goFile := filepath.Join(tmp, "go")
	outFile := filepath.Join(tmp, "out")
	logFile := filepath.Join(path, "agent.log")
	cmd := exec.Command("sh", "-c", `exec 3>>agent.log
while [ ! -f "$1" ]; do
  git status --porcelain >&3 || exit 1
  git rev-parse --abbrev-ref HEAD >&3 || exit 1
  sleep 0.05
done
echo still-working > wip.txt
git add wip.txt
git commit -m "agent kept working" >/dev/null
git rev-parse --abbrev-ref HEAD > "$2"
pwd >> "$2"`, "agent", goFile, outFile)
	cmd.Dir = path
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(logFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent loop never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "issue 418 feed")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.WriteFile(goFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("release process: %v", err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("agent after rename: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not finish after rename")
	}

	if _, err := os.Stat(filepath.Join(path, "wip.txt")); err != nil {
		t.Fatalf("spawn-time path lost the agent's write: %v", err)
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) < 2 {
		t.Fatalf("agent output = %q", got)
	}
	if lines[0] != newBranch {
		t.Fatalf("agent HEAD = %q, want %q", lines[0], newBranch)
	}
	if !sameResolvedPath(t, lines[1], path) {
		t.Fatalf("agent cwd = %q, want spawn path %q", lines[1], path)
	}
	ahead, err := driver.run(path, "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if ahead == "1" {
		t.Fatal("agent commit did not land on the spawn-time worktree")
	}
}

func TestRenameWorktreeBranchKeepsOpenFile(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	held := filepath.Join(path, "held.txt")
	f, err := os.OpenFile(held, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err := f.WriteString("before\n"); err != nil {
		t.Fatalf("write before: %v", err)
	}

	if _, err := driver.RenameWorktreeBranch(dir, path, branch, "open file"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := f.WriteString("after\n"); err != nil {
		t.Fatalf("write after rename: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := os.ReadFile(held)
	if err != nil {
		t.Fatalf("read spawn-time path: %v", err)
	}
	if string(got) != "before\nafter\n" {
		t.Fatalf("held file = %q", got)
	}
}

func TestRenameWorktreeBranchKeepsWorktreeList(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "listed")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	list, err := driver.Worktrees(dir)
	if err != nil {
		t.Fatalf("worktrees: %v", err)
	}
	found := false
	for _, wt := range list {
		if sameResolvedPath(t, wt.Root, path) {
			found = true
			if wt.Branch != newBranch {
				t.Fatalf("listed branch = %q, want %q", wt.Branch, newBranch)
			}
		}
	}
	if !found {
		t.Fatalf("spawn-time path missing from worktree list: %+v", list)
	}
}

func TestRenameWorktreeBranchOccupiesSpawnName(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := driver.RenameWorktreeBranch(dir, path, branch, "later name"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, _, err := driver.AddWorktree(dir, "claude-7a72"); err == nil {
		t.Fatal("spawn-time directory should still occupy that name")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("spawn-time directory moved: %v", err)
	}
}

func TestRenameWorktreeBranchThenRemoveIfClean(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "claude-7a72")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	newBranch, err := driver.RenameWorktreeBranch(dir, path, branch, "release the version")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	removed, err := driver.RemoveWorktreeIfClean(dir, path, newBranch)
	if err != nil || !removed {
		t.Fatalf("clean renamed worktree should remove: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("spawn-time directory still on disk")
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+newBranch); err == nil {
		t.Fatal("renamed branch should be deleted")
	}
}

func sameResolvedPath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatalf("resolve %q: %v", a, err)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		t.Fatalf("resolve %q: %v", b, err)
	}
	return ra == rb
}

func TestRemoveWorktreeKeepsDirtyAndAhead(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")

	dirtyPath, dirtyBranch, err := driver.AddWorktree(dir, "dirty")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	write(t, dirtyPath, "b.txt", "uncommitted")
	if removed, err := driver.RemoveWorktreeIfClean(dir, dirtyPath, dirtyBranch); err != nil || removed {
		t.Fatalf("dirty worktree must be kept: removed=%v err=%v", removed, err)
	}

	aheadPath, aheadBranch, err := driver.AddWorktree(dir, "ahead")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	write(t, aheadPath, "c.txt", "committed")
	commit(t, aheadPath, "work")
	if removed, err := driver.RemoveWorktreeIfClean(dir, aheadPath, aheadBranch); err != nil || removed {
		t.Fatalf("worktree with unmerged commits must be kept: removed=%v err=%v", removed, err)
	}
}

func TestRemoveWorktreeIfCleanBranchNotMergedIntoCurrentHEAD(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "c1")

	gitIn(t, dir, "branch", "feature")

	write(t, dir, "b.txt", "y")
	commit(t, dir, "c2")

	gitIn(t, dir, "checkout", "feature")

	path, branch, err := driver.AddWorktree(dir, "clean")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	removed, err := driver.RemoveWorktreeIfClean(dir, path, branch)
	if err != nil || !removed {
		t.Fatalf("clean worktree merged into base should remove even when main checkout sits elsewhere: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("worktree directory still on disk")
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("branch should be deleted")
	}
}

func TestRemoveWorktreeMissingDirKeeps(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	removed, err := driver.RemoveWorktreeIfClean(dir, filepath.Join(t.TempDir(), "gone"), "am/gone")
	if err != nil || removed {
		t.Fatalf("missing dir: removed=%v err=%v", removed, err)
	}
}
