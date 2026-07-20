# Diff Noise Implementation Plan (Phase 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop full-screen review from listing entries it cannot diff, and give untracked files real line counts instead of `+0 −0`.

**Architecture:** Three isolated changes down the existing pipeline. `git.ChangedFiles` stops emitting untracked directories. `diff.loadFile` derives a file's stat from the diff it just built when `numstat` had no entry for it. The review file list renders `binary` where a byte-count is meaningless.

**Tech Stack:** Go, `charmbracelet/bubbletea` + `lipgloss` for the TUI, the `git` CLI shelled out from `internal/git`.

## Global Constraints

- Work in the worktree `/tmp/am-rev` on branch `feat/review-ux`. Several agent sessions share the main checkout.
- Stage explicit paths. Never `git add -A`.
- Zero code comments unless a non-obvious WHY needs recording. Never describe WHAT the code does.
- No AI attribution in commit messages.
- `gofmt` clean, `go vet ./...` clean, `go test ./...` green before each commit.
- Spec: `docs/superpowers/specs/2026-07-20-review-target-and-noise-design.md`

---

### Task 1: Drop untracked directories

`git ls-files --others` emits a nested repository or worktree as a directory path with a trailing slash, because git will not descend into another repository. Those rows reach review as files, render `+0 −0`, and cannot be opened.

**Files:**
- Modify: `internal/git/git.go` (the `ScopeUncommitted` block in `ChangedFiles`, around line 272)
- Test: `internal/git/git_test.go`

**Interfaces:**
- Consumes: `testRepo(t)`, `write(t, dir, name, content)`, `commit(t, dir, message)` — existing helpers in `git_test.go`.
- Produces: no signature change. `ChangedFiles(root string, scope Scope, baseRef string) ([]ChangedFile, error)` keeps its shape and stops returning directory rows.

- [ ] **Step 1: Write the failing test**

Add to `internal/git/git_test.go`:

```go
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
```

Add `"strings"` to the test file's imports if it is not already there.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-rev && go test ./internal/git/ -run TestChangedFilesSkipsNestedRepoDirectories -v`
Expected: FAIL with `directory entry leaked into the file list: "nested/"`

- [ ] **Step 3: Write minimal implementation**

In `internal/git/git.go`, replace the untracked loop inside `ChangedFiles`:

```go
	if scope == ScopeUncommitted {
		untracked, err := d.run(root, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, err
		}
		for _, path := range splitNUL(untracked) {
			// git reports a nested repository as a directory it will not
			// descend into; there is nothing to diff and its changes belong
			// to that repository's own review.
			if strings.HasSuffix(path, "/") {
				continue
			}
			files = append(files, ChangedFile{Path: path, OldPath: path, Status: Untracked})
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-rev && go test ./internal/git/ -v`
Expected: PASS, including the pre-existing `TestUncommittedScope`

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-rev
git add internal/git/git.go internal/git/git_test.go
git commit -m "fix: drop untracked directories from the review file list"
```

---

### Task 2: Derive line counts for untracked files

`BuildSet` reads each file's stat from the `numstat` map. `git diff --numstat` covers only tracked changes, so untracked files take the zero value and render `+0 −0`. When a file has no `numstat` entry, count the `Add` and `Del` lines of the diff that was just built.

The count is derived only when `numstat` had no entry. A capped file's `Lines` are truncated, so for tracked files `numstat` stays authoritative.

**Files:**
- Modify: `internal/diff/diff.go` (`FileDiff` struct, `BuildSet` around line 100, `loadFile` around line 125)
- Test: `internal/diff/diff_test.go`

**Interfaces:**
- Consumes: `git.FileStat{Adds, Dels int; Binary bool}`, `Line{Kind LineKind}` with kinds `Add`, `Del`, `Same` — all already defined.
- Produces: `FileDiff` gains an unexported `statKnown bool` field, set by `BuildSet` from a comma-ok map read and consumed by `loadFile`. No exported signature changes.

- [ ] **Step 1: Write the failing test**

Add to `internal/diff/diff_test.go`:

```go
func TestUntrackedFileGetsLineCount(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "new.go", "package a\n\nfunc B() {}\n")
	write(t, dir, "empty.go", "")

	set, err := BuildSet(driver, dir, git.ScopeUncommitted)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileDiff{}
	for _, fd := range set.Files {
		byPath[fd.File.Path] = fd
	}
	if got := byPath["new.go"].Stat.Adds; got != 3 {
		t.Errorf("new.go adds = %d, want 3", got)
	}
	if got := byPath["new.go"].Stat.Dels; got != 0 {
		t.Errorf("new.go dels = %d, want 0", got)
	}
	if got := byPath["empty.go"].Stat.Adds; got != 0 {
		t.Errorf("empty.go adds = %d, want 0", got)
	}
}
```

If `testRepo`, `write`, and `commit` do not exist in `internal/diff/diff_test.go`, copy the versions from `internal/git/git_test.go` into it, renaming nothing.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-rev && go test ./internal/diff/ -run TestUntrackedFileGetsLineCount -v`
Expected: FAIL with `new.go adds = 0, want 3`

- [ ] **Step 3: Write minimal implementation**

In `internal/diff/diff.go`, add the field to `FileDiff` (next to the existing `Err error` field):

```go
	statKnown bool
```

In `BuildSet`, replace the loop body that builds each `FileDiff`:

```go
	for i, file := range files {
		stat, known := stats[file.Path]
		fd := FileDiff{File: file, Stat: stat, statKnown: known}
		if i < maxEagerFiles {
			loadFile(driver, repo.Root, scope, baseRef, &fd)
		}
		set.Files = append(set.Files, fd)
	}
```

In `loadFile`, replace the final `BuildFile` line:

```go
	known := fd.statKnown
	*fd = BuildFile(oldContent, newContent, fd.File, fd.Stat)
	fd.statKnown = known
	if !known {
		fd.Stat = countStat(fd.Lines)
	}
```

Add the helper below `loadFile`:

```go
func countStat(lines []Line) git.FileStat {
	var stat git.FileStat
	for _, line := range lines {
		switch line.Kind {
		case Add:
			stat.Adds++
		case Del:
			stat.Dels++
		}
	}
	return stat
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-rev && go test ./internal/diff/ -v`
Expected: PASS, including pre-existing tests

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-rev
git add internal/diff/diff.go internal/diff/diff_test.go
git commit -m "fix: count lines for untracked files instead of showing zero"
```

---

### Task 3: Label binary files in the review list

A binary file has no line counts, so `+0 −0` reads as "no changes" rather than "not countable". `loadFile` already sets `Binary`; the list just needs to render it.

**Files:**
- Modify: `internal/ui/diffview.go` (`viewDiffFileList`, the `counts` assignment around line 1119)
- Test: `internal/ui/review_regression_test.go`

**Interfaces:**
- Consumes: `FileDiff.Binary bool` from `internal/diff`, `mutedStyle` and `colorFinished`/`colorErrored` styles already defined in the `ui` package.
- Produces: no signature change. `viewDiffFileList(width, height int) string` renders `binary` in place of the counts for binary rows.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/review_regression_test.go`:

```go
func TestBinaryFileShowsBinaryNotZeroCounts(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "binary", dir)

	rendered := m.viewDiffFileList(60, 20)
	if !strings.Contains(rendered, "binary") {
		t.Fatalf("binary file should be labelled binary, got:\n%s", rendered)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-rev && go test ./internal/ui/ -run TestBinaryFileShowsBinaryNotZeroCounts -v`
Expected: FAIL with `binary file should be labelled binary`

- [ ] **Step 3: Write minimal implementation**

In `internal/ui/diffview.go`, replace the `counts` assignment inside `viewDiffFileList`:

```go
		counts := lipgloss.NewStyle().Foreground(colorFinished).Render(fmt.Sprintf("+%d", fd.Stat.Adds)) +
			" " + lipgloss.NewStyle().Foreground(colorErrored).Render(fmt.Sprintf("−%d", fd.Stat.Dels))
		if fd.Binary {
			counts = mutedStyle.Render("binary")
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-rev && go test ./internal/ui/ -v -run 'TestBinary|TestReview|TestDiff'`
Expected: PASS

- [ ] **Step 5: Verify the whole suite and formatting**

Run:
```bash
cd /tmp/am-rev
gofmt -l internal/
go vet ./...
go test ./...
```
Expected: `gofmt` prints nothing, `go vet` prints nothing, every package reports `ok`

- [ ] **Step 6: Commit**

```bash
cd /tmp/am-rev
git add internal/ui/diffview.go internal/ui/review_regression_test.go
git commit -m "fix: label binary files in the review list"
```

---

### Task 4: Verify against the real repository and open the PR

The bug was found on `mreshet/servers/gma2_master_node`, which has four nested worktree directories and several untracked screenshots. Confirm the fix there before opening the PR.

**Files:**
- No source changes. Verification and PR only.

**Interfaces:**
- Consumes: the `ChangedFiles` behaviour from Task 1 and the stats from Task 2.
- Produces: a pull request against `main`.

- [ ] **Step 1: Confirm the nested directories are gone**

Run:
```bash
cd /tmp/am-rev
cat > internal/git/zz_real_test.go <<'EOF'
package git

import (
	"fmt"
	"strings"
	"testing"
)

func TestZZRealRepo(t *testing.T) {
	d, _ := New()
	files, err := d.ChangedFiles("/Users/yoan/Desktop/projects/mreshet/servers/gma2_master_node", ScopeUncommitted, "")
	if err != nil {
		t.Skip(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "/") {
			t.Errorf("directory still listed: %s", f.Path)
		}
	}
	fmt.Printf("%d entries, none of them directories\n", len(files))
}
EOF
go test ./internal/git/ -run TestZZRealRepo -v
rm internal/git/zz_real_test.go
```
Expected: no `directory still listed` errors, and the count drops from 20 by the four `.worktrees/GMN-*` rows

- [ ] **Step 2: Push the branch**

```bash
cd /tmp/am-rev
git push -u origin feat/review-ux
```

- [ ] **Step 3: Open the pull request**

```bash
cd /tmp/am-rev
gh pr create --title "fix: stop review listing entries it cannot diff" --body "$(cat <<'BODY'
## Problem

Review listed 20 files for a session where only 2 had reviewable changes. The other 18 rendered `+0 −0`:

- `.worktrees/GMN-3117/` and three siblings are **directories**. `git ls-files --others` reports a nested repository as a directory because git will not descend into another repo. There is nothing to diff.
- Untracked screenshots are **binary**, so no line counts exist.
- Untracked text files showed `+0 −0` too, because `git diff --numstat` covers only tracked changes.

The header total was wrong for the same reason: "20 files · +8 −4" counted only the 2 tracked files.

## Fix

- Skip untracked directory entries; a nested repo's changes belong to its own review.
- When a file has no `numstat` entry, derive its counts from the diff just built. Tracked files keep using `numstat`, which stays authoritative for capped files.
- Render `binary` instead of `+0 −0` where a line count is meaningless.

Header totals correct themselves once the per-file stats are right.

## Tests

- A nested repository is omitted while untracked files are still listed.
- An untracked text file reports its line count; an empty one stays at `+0 −0`.
- A binary file is labelled `binary`.

Full suite, `go vet`, `gofmt` clean.
BODY
)"
```
Expected: the command prints the new pull request URL

---

## Self-Review

**Spec coverage.** The spec's Phase 1 lists three defects. Untracked directories map to Task 1, missing line counts to Task 2, binary labelling to Task 3. The spec's claim about header totals is verified by `TestHeaderTotalStableWithoutLazyLoad` in `internal/diff/diff_test.go`, which asserts every listed file carries its stat straight out of `BuildSet`, past the eager-load cap and with no lazy load, so the total the header sums is correct on the first paint and does not drift as the user scrolls. The spec's Phase 1 test list maps to the tests in Tasks 1 through 3, plus the real-repository check in Task 4.

**Placeholders.** Every code step carries the actual code. No TBD, no "handle edge cases", no "similar to Task N".

**Type consistency.** `Driver.CountWorkingLines` returns `git.LineCount` and is called only from `countUnknownStat`, which runs in `BuildSet` and writes `git.FileStat`. `statKnown` is written in `BuildSet`, preserved across `loadFile`, and read through `FileDiff.StatKnown` in `viewDiffFileList`. `ChangedFile.Path` is the field filtered in Task 1 and asserted in its test. `FileDiff.Binary` is set in `loadFile` and read in `viewDiffFileList`.

One gap found and closed: Task 2's test needs `testRepo`/`write`/`commit` helpers, which live in `internal/git/git_test.go` and may not exist in `internal/diff/diff_test.go`. Step 1 now says to copy them if missing.
