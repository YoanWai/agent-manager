package review

import (
	"fmt"
	"strings"
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/charmbracelet/x/ansi"
)

func TestHighlightFileBothSides(t *testing.T) {
	fd := diff.BuildFile(
		[]byte("package a\n\nfunc A() {}\n"),
		[]byte("package a\n\nfunc A() int { return 1 }\n"),
		git.ChangedFile{Path: "a.go", OldPath: "a.go", Status: git.Modified}, git.FileStat{})
	hl := HighlightFile(&fd)
	if hl == nil {
		t.Fatal("highlight is nil")
	}
	if first := hl.Line(0, ""); !strings.Contains(first, "\x1b[") {
		t.Fatalf("go source should highlight: %q", first)
	}
	assertHighlightMatchesText(t, &fd, hl)
}

// A hunk-only model skips file lines, so highlighting keyed by line number
// would colour every line after the first gap with another line's tokens.
func TestHighlightFileHunkModel(t *testing.T) {
	fd := bigEditedFile(t)
	if fd.Lines[0].Kind != diff.Gap {
		t.Fatalf("want a hunk model opening with a gap, got %d lines", len(fd.Lines))
	}
	hl := HighlightFile(&fd)
	var highlighted strings.Builder
	for i := range fd.Lines {
		highlighted.WriteString(hl.Line(i, ""))
	}
	if !strings.Contains(highlighted.String(), "\x1b[") {
		t.Fatal("go source should highlight")
	}
	assertHighlightMatchesText(t, &fd, hl)
}

// bigEditedFile is one edit at line 11002 of a file past the whole-file
// line threshold, so it can only render through the hunk model.
func bigEditedFile(t *testing.T) diff.FileDiff {
	t.Helper()
	var oldText strings.Builder
	oldText.WriteString("package a\n")
	for i := 0; i < 12000; i++ {
		fmt.Fprintf(&oldText, "var name%d = %q\n", i, fmt.Sprintf("value %d", i))
	}
	newText := strings.Replace(oldText.String(), `var name11000 = "value 11000"`, `var name11000 = "edited"`, 1)
	fd := diff.BuildFile([]byte(oldText.String()), []byte(newText),
		git.ChangedFile{Path: "a.go", OldPath: "a.go", Status: git.Modified}, git.FileStat{Adds: 1, Dels: 1})
	if fd.Err != nil {
		t.Fatal(fd.Err)
	}
	if len(fd.Lines) == 0 {
		t.Fatal("the hunk model is empty")
	}
	return fd
}

func assertHighlightMatchesText(t *testing.T, fd *diff.FileDiff, hl *Highlight) {
	t.Helper()
	for i, line := range fd.Lines {
		if got := ansi.Strip(hl.Line(i, line.Text)); got != line.Text {
			t.Fatalf("line %d highlight drifted: %q vs %q", i, got, line.Text)
		}
	}
}

func BenchmarkHighlightLargeHunkModel(b *testing.B) {
	fd := diff.BuildFile(nil, []byte(strings.Repeat(strings.Repeat("x", 4000)+"\n", 8000)),
		git.ChangedFile{Path: "large.txt", Status: git.Added}, git.FileStat{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		HighlightFile(&fd)
	}
}
