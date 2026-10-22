package model

import (
	"fmt"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"strings"
	"testing"
	"unicode/utf8"
)

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

// A big file with one small edit shows that edit with its real line numbers,
// the context around it, and gap markers for everything left out.
func TestHunkModelKeepsRealLineNumbers(t *testing.T) {
	const total = maxWholeFileLines + 2000
	oldText := linesOf(total)
	newText := strings.Replace(oldText, "line 11000\n", "line 11000 edited\n", 1)
	fd := buildTestFile(t, oldText, newText)

	if fd.Truncated {
		t.Error("a small edit in a big file fits the hunk model")
	}
	if fd.OldTotal != total || fd.NewTotal != total {
		t.Errorf("totals = %d/%d, want %d", fd.OldTotal, fd.NewTotal, total)
	}
	if len(fd.Lines) > 2*hunkContext+4 {
		t.Errorf("model = %d lines, want one small hunk", len(fd.Lines))
	}
	if fd.Lines[0].Kind != Gap {
		t.Errorf("first line = %v, want a gap for the skipped head", fd.Lines[0].Kind)
	}
	var edited *Line
	for i := range fd.Lines {
		if fd.Lines[i].Kind == Add {
			edited = &fd.Lines[i]
		}
	}
	if edited == nil {
		t.Fatal("the edit is missing from the model")
	}
	// linesOf numbers from zero, so "line 11000" is the file's 11001st line.
	const wantNum = 11001
	if edited.NewNum != wantNum {
		t.Errorf("edited line number = %d, want %d", edited.NewNum, wantNum)
	}
	if len(fd.Changes) != 1 {
		t.Errorf("changes = %v, want one jump target", fd.Changes)
	}
}

// A file whose hunks outgrow the model is cut off with a marker rather than
// building a model no one can scroll.
func TestHunkModelCapsAndMarksTruncation(t *testing.T) {
	const total = maxHunkModelLines + 2000
	fd := buildTestFile(t, "", linesOf(total))

	if !fd.Truncated {
		t.Error("a model over the cap is truncated")
	}
	if len(fd.Lines) != maxHunkModelLines+1 {
		t.Errorf("model = %d lines, want the cap plus a marker", len(fd.Lines))
	}
	last := fd.Lines[len(fd.Lines)-1]
	if last.Kind != Gap {
		t.Errorf("last line = %v, want a gap marker", last.Kind)
	}
}

// A minified line would otherwise wrap into thousands of visual rows.
func TestHunkModelCapsLongLines(t *testing.T) {
	oldText := strings.Repeat("x", maxWholeFileBytes+10) + "\n"
	fd := buildTestFile(t, oldText, oldText+"tail\n")

	for _, line := range fd.Lines {
		if len(line.Text) > maxHunkLineBytes+len("…") {
			t.Fatalf("line kept %d bytes, want it capped", len(line.Text))
		}
	}
}

func TestHunkModelUnchangedLargeFile(t *testing.T) {
	for _, text := range []string{linesOf(maxWholeFileLines + 1), strings.Repeat("x", maxWholeFileBytes+1)} {
		fd := buildTestFile(t, text, text)
		if len(fd.Lines) != 1 || fd.Lines[0].Kind != Gap || fd.Truncated || len(fd.Changes) != 0 {
			t.Fatalf("unchanged large file should show only an unchanged-lines marker: %+v", fd)
		}
	}
}

func TestHunkModelMultipleHunks(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("newline-%q", newline), func(t *testing.T) {
			oldText := strings.ReplaceAll(linesOf(12000), "\n", newline)
			newText := "inserted" + newline + oldText
			newText = strings.Replace(newText, "line 6000"+newline, "", 1)
			newText = strings.TrimSuffix(newText, newline) + newline + "tail"
			fd := buildTestFile(t, oldText, newText)
			oldLines := strings.Split(strings.TrimSuffix(oldText, newline), newline)
			newLines := strings.Split(newText, newline)
			gaps := 0
			for _, line := range fd.Lines {
				if line.Kind == Gap {
					gaps++
					continue
				}
				if line.OldNum > 0 && oldLines[line.OldNum-1] != line.Text {
					t.Fatalf("old line number drifted: %+v", line)
				}
				if line.NewNum > 0 && newLines[line.NewNum-1] != line.Text {
					t.Fatalf("new line number drifted: %+v", line)
				}
			}
			if gaps != 2 || len(fd.Changes) != 3 || fd.OldTotal != 12000 || fd.NewTotal != 12001 {
				t.Fatalf("gaps=%d changes=%v totals=%d/%d", gaps, fd.Changes, fd.OldTotal, fd.NewTotal)
			}
		})
	}
}

func TestHunkModelLineCapPreservesUTF8(t *testing.T) {
	text := strings.Repeat("界", maxWholeFileBytes/3+1)
	fd := buildTestFile(t, "", text)
	if len(fd.Lines) != 1 || !utf8.ValidString(fd.Lines[0].Text) || !strings.HasSuffix(fd.Lines[0].Text, "…") {
		t.Fatal("capped line must preserve complete Unicode characters and show an ellipsis")
	}
}

func linesOf(count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func hasLine(fd FileDiff, kind LineKind, text string) bool {
	for _, line := range fd.Lines {
		if line.Kind == kind && line.Text == text {
			return true
		}
	}
	return false
}
