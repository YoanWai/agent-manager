package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func testRepo(t *testing.T) (*Driver, string) {
	t.Helper()
	driver, err := New()
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
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
	return driver, dir
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
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
