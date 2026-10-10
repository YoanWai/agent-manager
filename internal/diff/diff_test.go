package diff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
	"golang.org/x/sys/unix"
)

const (
	maxWholeFileBytes = 1 << 20
	maxWholeFileLines = 10000
)

func testRepo(t *testing.T) (*git.Driver, string) {
	t.Helper()
	driver, err := git.New()
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

func TestUntrackedFileGetsLineCount(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "new.go", "package a\n\nfunc B() {}\n")
	write(t, dir, "empty.go", "")

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := range set.Files {
		ensureFile(driver, &set, i)
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
	empty := byPath["empty.go"]
	if !empty.StatKnown() {
		t.Error("empty.go stat should be known, not an unknown count rendered as zero")
	}
	if empty.Stat.Adds != 0 || empty.Stat.Dels != 0 {
		t.Errorf("empty.go stat = %+v, want zero", empty.Stat)
	}
}

func linesOf(count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// Regression guard: the capped model would report maxWholeFileLines here.
func TestUntrackedFileOverLineCapCountsTrueLines(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")

	const total = maxWholeFileLines + 2000
	write(t, dir, "huge.txt", linesOf(total))

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	var huge *FileDiff
	for i := range set.Files {
		if set.Files[i].File.Path == "huge.txt" {
			huge = &set.Files[i]
		}
	}
	if huge == nil {
		t.Fatal("huge.txt missing from the set")
	}
	ensureFile(driver, &set, 0)
	huge = &set.Files[0]
	if !huge.StatKnown() {
		t.Fatal("huge.txt stat should be known")
	}
	if huge.Stat.Adds != total {
		t.Errorf("huge.txt adds = %d, want %d (the capped model would say %d)", huge.Stat.Adds, total, maxWholeFileLines)
	}
	if !huge.Truncated {
		t.Error("huge.txt should still be marked truncated for display")
	}
}

// Regression guard: a file past the whole-file byte threshold still counts
// its lines, and still diffs through the hunk model.
func TestUntrackedFileOverByteThresholdStillCounts(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")

	line := strings.Repeat("x", 200) + "\n"
	total := (maxWholeFileBytes / len(line)) + 500
	write(t, dir, "wide.txt", strings.Repeat(line, total))

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	var wide *FileDiff
	for i := range set.Files {
		if set.Files[i].File.Path == "wide.txt" {
			wide = &set.Files[i]
		}
	}
	if wide == nil {
		t.Fatal("wide.txt missing from the set")
	}
	ensureFile(driver, &set, 0)
	wide = &set.Files[0]
	if !wide.StatKnown() {
		t.Fatal("wide.txt stat should be known")
	}
	if wide.Stat.Adds != total {
		t.Errorf("wide.txt adds = %d, want %d", wide.Stat.Adds, total)
	}
	if len(wide.Lines) == 0 {
		t.Error("wide.txt should still diff through the hunk model")
	}
}

// Invariant: numstat wins; the hunk model must not overwrite it. The edit
// sits past the whole-file line threshold, so it is only visible at all
// because the file falls back to hunks.
func TestHunkModelKeepsNumstat(t *testing.T) {
	driver, dir := testRepo(t)
	const total = maxWholeFileLines + 2000
	write(t, dir, "big.txt", linesOf(total))
	commit(t, dir, "init")

	changed := strings.Replace(linesOf(total), "line 11000\n", "line 11000 edited\n", 1)
	if changed == linesOf(total) {
		t.Fatal("test setup failed to change a line past the cap")
	}
	write(t, dir, "big.txt", changed)

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(set.Files))
	}
	ensureFile(driver, &set, 0)
	big := set.Files[0]
	if !big.StatKnown() {
		t.Fatal("big.txt stat should stay known after the truncated load")
	}
	if big.Stat.Adds != 1 || big.Stat.Dels != 1 {
		t.Errorf("big.txt stat = %+v, want 1 add 1 del from numstat", big.Stat)
	}
	if !hasLine(big, Add, "line 11000 edited") {
		t.Error("the edited line past the line threshold should be in the model")
	}
}

func hasLine(fd FileDiff, kind LineKind, text string) bool {
	for _, line := range fd.Lines {
		if line.Kind == kind && line.Text == text {
			return true
		}
	}
	return false
}

func TestUntrackedStatsFillAtBuildWithoutLoadingContents(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")

	const files, linesEach = 205, 3
	for i := 0; i < files; i++ {
		write(t, dir, fmt.Sprintf("untracked%03d.txt", i), linesOf(linesEach))
	}

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != files {
		t.Fatalf("files = %d, want %d", len(set.Files), files)
	}

	adds := 0
	for _, fd := range set.Files {
		if fd.Loaded() {
			t.Fatalf("%s should keep its contents deferred after BuildSet", fd.File.Path)
		}
		if !fd.StatKnown() || fd.Stat.Adds != linesEach {
			t.Errorf("%s stat = %+v known=%v, want %d adds",
				fd.File.Path, fd.Stat, fd.StatKnown(), linesEach)
		}
		adds += fd.Stat.Adds
	}
	if want := files * linesEach; adds != want {
		t.Fatalf("total adds = %d, want %d", adds, want)
	}
}

func TestUntrackedStatErrorIsRecorded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file regardless of mode")
	}
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "locked.go", "package a\n")
	locked := filepath.Join(dir, "locked.go")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(set.Files))
	}
	fd := set.Files[0]
	if fd.Err == nil {
		t.Fatal("failed eager count did not reach the file diff")
	}
	if fd.StatKnown() || fd.Loaded() {
		t.Fatalf("failed count marked the file known or loaded: %+v", fd)
	}
}

func TestBuildSetSkipsUntrackedSpecialFiles(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "regular.go", "package a\n")
	if err := unix.Mkfifo(filepath.Join(dir, "blocked.pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.go")); err != nil {
		t.Fatal(err)
	}

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 || set.Files[0].File.Path != "regular.go" {
		t.Fatalf("files = %+v, want only regular.go", set.Files)
	}
}

func TestUnreadableUntrackedFileDoesNotAbortSet(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file regardless of mode")
	}
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "readable.go", "package a\n\nfunc B() {}\n")
	write(t, dir, "locked.go", "package a\n")
	locked := filepath.Join(dir, "locked.go")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatalf("BuildSet aborted on one unreadable file: %v", err)
	}
	for i := range set.Files {
		ensureFile(driver, &set, i)
	}
	byPath := map[string]FileDiff{}
	for _, fd := range set.Files {
		byPath[fd.File.Path] = fd
	}
	readable, ok := byPath["readable.go"]
	if !ok {
		t.Fatal("readable.go missing from set")
	}
	if !readable.StatKnown() || readable.Stat.Adds != 3 {
		t.Errorf("readable.go stat = %+v known=%v, want 3 adds", readable.Stat, readable.StatKnown())
	}
	bad, ok := byPath["locked.go"]
	if !ok {
		t.Fatal("locked.go missing from set")
	}
	if bad.Err == nil {
		t.Error("locked.go should carry the count error")
	}
	if bad.StatKnown() {
		t.Error("locked.go stat should stay unknown so the row renders ?")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// A stored base ref drives the vs-base diff: the branch diffs against the
// override's merge base, not the auto-detected main.
func ensureFile(driver *git.Driver, set *Set, index int) {
	if set.Files[index].Loaded() {
		return
	}
	set.Files[index] = LoadFile(driver, *set, index)
}

func TestBuildSetBranchBaseOverride(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	commit(t, dir, "c1")
	gitRun(t, dir, "branch", "feature")
	write(t, dir, "a.go", "package a\n\nfunc A() int { return 2 }\n")
	commit(t, dir, "c2")

	auto, err := BuildSet(driver, dir, git.ScopeBranch, "")
	if err != nil {
		t.Fatalf("auto BuildSet: %v", err)
	}
	if len(auto.Files) != 0 {
		t.Fatalf("auto base (main == HEAD) should show no changes, got %d files", len(auto.Files))
	}

	over, err := BuildSet(driver, dir, git.ScopeBranch, "feature")
	if err != nil {
		t.Fatalf("override BuildSet: %v", err)
	}
	if over.BaseOverride != "feature" {
		t.Errorf("BaseOverride = %q, want feature", over.BaseOverride)
	}
	if !strings.HasPrefix(over.BaseDesc, "feature@") {
		t.Errorf("BaseDesc = %q, want feature@<short>", over.BaseDesc)
	}
	found := false
	for _, fd := range over.Files {
		if fd.File.Path == "a.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("override base should surface a.go changed since the fork, files = %+v", over.Files)
	}
}

// An unresolvable stored base fails loudly, naming the ref, never falling back
// to auto-detection.
func TestBuildSetBranchBaseOverrideInvalid(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "c1")

	_, err := BuildSet(driver, dir, git.ScopeBranch, "no-such-ref")
	if err == nil {
		t.Fatal("invalid base should error, not fall back to main")
	}
	if !strings.Contains(err.Error(), "no-such-ref") {
		t.Errorf("error must name the ref, got %q", err)
	}
}
