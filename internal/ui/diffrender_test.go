package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/charmbracelet/x/ansi"
)

func TestWrapTintedPreservesText(t *testing.T) {
	rows := wrapTinted("func main() {", nil, bgAdd, bgAddSpan, 40)
	if len(rows) != 1 {
		t.Fatalf("short line should be one row: %d", len(rows))
	}
	// A tinted row pads to width so the background fills the column.
	if got := strings.TrimRight(ansi.Strip(rows[0]), " "); got != "func main() {" {
		t.Fatalf("text mangled: %q", got)
	}
	if !strings.HasPrefix(rows[0], bgAdd) {
		t.Fatalf("background not opened: %q", rows[0])
	}
}

func TestWrapTintedReemitsBackgroundAfterReset(t *testing.T) {
	rows := wrapTinted("\x1b[38;5;197mfunc\x1b[0m main", nil, bgDel, bgDelSpan, 40)
	tinted := rows[0]
	if !strings.HasPrefix(ansi.Strip(tinted), "func main") {
		t.Fatalf("text mangled: %q", ansi.Strip(tinted))
	}
	resetAt := strings.Index(tinted, "\x1b[0m")
	if resetAt < 0 || !strings.HasPrefix(tinted[resetAt+4:], bgDel) {
		t.Fatalf("background must be re-opened after chroma reset: %q", tinted)
	}
}

func TestWrapTintedSpanSwitch(t *testing.T) {
	rows := wrapTinted("if t <= exp", []diff.Span{{Start: 5, End: 7}}, bgAdd, bgAddSpan, 40)
	if !strings.Contains(rows[0], bgAddSpan) {
		t.Fatalf("span background missing: %q", rows[0])
	}
	if !strings.HasPrefix(ansi.Strip(rows[0]), "if t <= exp") {
		t.Fatalf("text mangled: %q", ansi.Strip(rows[0]))
	}
}

func TestWrapTintedWrapsLongLine(t *testing.T) {
	long := strings.Repeat("x", 100)
	rows := wrapTinted(long, nil, "", "", 20)
	if len(rows) != 5 {
		t.Fatalf("100 cols / 20 = 5 rows, got %d", len(rows))
	}
	var joined string
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 20 {
			t.Fatalf("row exceeds width: %d", w)
		}
		joined += ansi.Strip(row)
	}
	if joined != long {
		t.Fatalf("wrap lost content: %q", joined)
	}
}

func TestHighlightFileBothSides(t *testing.T) {
	fd := diff.BuildFile(
		[]byte("package a\n\nfunc A() {}\n"),
		[]byte("package a\n\nfunc A() int { return 1 }\n"),
		git.ChangedFile{Path: "a.go", OldPath: "a.go", Status: git.Modified}, git.FileStat{})
	hl := highlightFile(&fd)
	if hl == nil || len(hl.newLines) != 3 || len(hl.oldLines) != 3 {
		t.Fatalf("hl sides = %d/%d", len(hl.oldLines), len(hl.newLines))
	}
	if !strings.Contains(hl.newLines[0], "\x1b[") {
		t.Fatalf("go source should highlight: %q", hl.newLines[0])
	}
	for _, line := range fd.Lines {
		if ansi.Strip(hl.hlLine(line)) != line.Text {
			t.Fatalf("highlight text drifted: %q vs %q", ansi.Strip(hl.hlLine(line)), line.Text)
		}
	}
}

func TestHLCacheEvicts(t *testing.T) {
	cache := newHLCache()
	for i := 0; i < highlightCacheCap+3; i++ {
		cache.put(hlKey{path: string(rune('a' + i))}, &fileHL{})
	}
	if len(cache.entries) != highlightCacheCap {
		t.Fatalf("cache size = %d", len(cache.entries))
	}
}
