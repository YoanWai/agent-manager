package diff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
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

func buildTestFile(t *testing.T, oldText, newText string) FileDiff {
	t.Helper()
	fd := BuildFile([]byte(oldText), []byte(newText), git.ChangedFile{Path: "f.go", OldPath: "f.go", Status: git.Modified}, git.FileStat{})
	if fd.Err != nil {
		t.Fatal(fd.Err)
	}
	return fd
}

func TestWholeFileModel(t *testing.T) {
	oldText := "line1\nline2\nline3\nline4\n"
	newText := "line1\nline2 changed\nline3\nline4\nline5\n"
	fd := buildTestFile(t, oldText, newText)

	kinds := []LineKind{}
	for _, line := range fd.Lines {
		kinds = append(kinds, line.Kind)
	}
	want := []LineKind{Same, Del, Add, Same, Same, Add}
	if len(kinds) != len(want) {
		t.Fatalf("lines = %+v", fd.Lines)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v want %v", kinds, want)
		}
	}
	if fd.Lines[0].OldNum != 1 || fd.Lines[0].NewNum != 1 {
		t.Fatalf("first line numbering: %+v", fd.Lines[0])
	}
	if fd.Lines[1].OldNum != 2 || fd.Lines[1].NewNum != 0 {
		t.Fatalf("del numbering: %+v", fd.Lines[1])
	}
	if fd.Lines[2].NewNum != 2 || fd.Lines[2].OldNum != 0 {
		t.Fatalf("add numbering: %+v", fd.Lines[2])
	}
	if fd.OldTotal != 4 || fd.NewTotal != 5 {
		t.Fatalf("totals = %d/%d", fd.OldTotal, fd.NewTotal)
	}
}

func TestPairingAndSpans(t *testing.T) {
	fd := buildTestFile(t, "if t < exp {\n", "if t <= exp {\n")
	if len(fd.Lines) != 2 {
		t.Fatalf("lines = %+v", fd.Lines)
	}
	del, add := fd.Lines[0], fd.Lines[1]
	if del.Pair != 1 || add.Pair != 0 {
		t.Fatalf("pairs = %d, %d", del.Pair, add.Pair)
	}
	if len(add.Spans) == 0 {
		t.Fatal("modified line pair should carry word spans")
	}
}

func TestUnchangedFileKeepsAllLines(t *testing.T) {
	text := "a\nb\nc\n"
	fd := buildTestFile(t, text, text)
	if len(fd.Lines) != 3 {
		t.Fatalf("unchanged file should keep all lines: %+v", fd.Lines)
	}
	for _, line := range fd.Lines {
		if line.Kind != Same {
			t.Fatalf("unexpected kind: %+v", line)
		}
	}
}

func TestAddedFile(t *testing.T) {
	fd := buildTestFile(t, "", "one\ntwo\n")
	if len(fd.Lines) != 2 {
		t.Fatalf("lines = %+v", fd.Lines)
	}
	for _, line := range fd.Lines {
		if line.Kind != Add {
			t.Fatalf("added file lines should all be Add: %+v", line)
		}
	}
	if len(fd.Changes) != 1 || fd.Changes[0] != 0 {
		t.Fatalf("changes = %v", fd.Changes)
	}
}

func TestSideBySideRows(t *testing.T) {
	fd := buildTestFile(t, "a\nb\nc\n", "a\nB\nB2\nc\n")
	rows := fd.SideBySideRows()
	// a same, (b -> B, B2) block: 1 del vs 2 adds = 2 rows, c same.
	if len(rows) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[1].Left < 0 || rows[1].Right < 0 {
		t.Fatalf("paired row should fill both sides: %+v", rows[1])
	}
	if rows[2].Left != -1 || rows[2].Right < 0 {
		t.Fatalf("surplus add row should blank the left: %+v", rows[2])
	}
}

func TestTabsExpanded(t *testing.T) {
	fd := buildTestFile(t, "", "\tindented\n")
	if fd.Lines[0].Text != "    indented" {
		t.Fatalf("text = %q", fd.Lines[0].Text)
	}
}

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

// Regression guard: the capped model would report maxFileLines here.
func TestUntrackedFileOverLineCapCountsTrueLines(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")

	const total = maxFileLines + 2000
	write(t, dir, "huge.txt", linesOf(total))

	set, err := BuildSet(driver, dir, git.ScopeUncommitted)
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
	if !huge.StatKnown() {
		t.Fatal("huge.txt stat should be known")
	}
	if huge.Stat.Adds != total {
		t.Errorf("huge.txt adds = %d, want %d (the capped model would say %d)", huge.Stat.Adds, total, maxFileLines)
	}
	if !huge.Truncated {
		t.Error("huge.txt should still be marked truncated for display")
	}
}

// Regression guard: the byte cap stops the diff model, not the count.
func TestUntrackedFileOverByteCapStillCounts(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")

	line := strings.Repeat("x", 200) + "\n"
	total := (maxFileBytes / len(line)) + 500
	write(t, dir, "wide.txt", strings.Repeat(line, total))

	set, err := BuildSet(driver, dir, git.ScopeUncommitted)
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
	if !wide.StatKnown() {
		t.Fatal("wide.txt stat should be known")
	}
	if wide.Stat.Adds != total {
		t.Errorf("wide.txt adds = %d, want %d", wide.Stat.Adds, total)
	}
	if !wide.Truncated {
		t.Error("wide.txt should be marked too large to diff")
	}
}

// Invariant: numstat wins; the truncated model must not overwrite it.
func TestTruncatedTrackedFileKeepsNumstat(t *testing.T) {
	driver, dir := testRepo(t)
	const total = maxFileLines + 2000
	write(t, dir, "big.txt", linesOf(total))
	commit(t, dir, "init")

	changed := strings.Replace(linesOf(total), "line 11000\n", "line 11000 edited\n", 1)
	if changed == linesOf(total) {
		t.Fatal("test setup failed to change a line past the cap")
	}
	write(t, dir, "big.txt", changed)

	set, err := BuildSet(driver, dir, git.ScopeUncommitted)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(set.Files))
	}
	big := set.Files[0]
	if big.Stat.Adds != 1 || big.Stat.Dels != 1 {
		t.Errorf("big.txt stat = %+v, want 1 add 1 del from numstat", big.Stat)
	}
}

// The header sums every file's Stat, so all must be set before any lazy load.
func TestHeaderTotalStableWithoutLazyLoad(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")

	const files, linesEach = maxEagerFiles + 5, 3
	for i := 0; i < files; i++ {
		write(t, dir, fmt.Sprintf("untracked%03d.txt", i), linesOf(linesEach))
	}

	set, err := BuildSet(driver, dir, git.ScopeUncommitted)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != files {
		t.Fatalf("files = %d, want %d", len(set.Files), files)
	}

	adds := 0
	for i := range set.Files {
		fd := set.Files[i]
		if !fd.StatKnown() {
			t.Fatalf("%s has no stat after BuildSet", fd.File.Path)
		}
		if fd.Stat.Adds != linesEach {
			t.Errorf("%s adds = %d, want %d (index %d)", fd.File.Path, fd.Stat.Adds, linesEach, i)
		}
		adds += fd.Stat.Adds
	}
	if want := files * linesEach; adds != want {
		t.Fatalf("total adds = %d, want %d", adds, want)
	}

	// Loading the files the eager pass skipped must not move the total.
	for i := range set.Files {
		EnsureFile(driver, &set, i)
	}
	after := 0
	for _, fd := range set.Files {
		after += fd.Stat.Adds
	}
	if after != adds {
		t.Fatalf("total drifted after lazy loading: %d -> %d", adds, after)
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

	set, err := BuildSet(driver, dir, git.ScopeUncommitted)
	if err != nil {
		t.Fatalf("BuildSet aborted on one unreadable file: %v", err)
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
