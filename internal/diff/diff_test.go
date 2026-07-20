package diff

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if got := byPath["empty.go"].Stat.Adds; got != 0 {
		t.Errorf("empty.go adds = %d, want 0", got)
	}
}
