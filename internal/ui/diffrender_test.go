package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/charmbracelet/x/ansi"
)

func TestTintLinePreservesText(t *testing.T) {
	plain := "func main() {"
	tinted := tintLine(plain, nil, bgAdd, bgAddSpan)
	if ansi.Strip(tinted) != plain {
		t.Fatalf("text mangled: %q", ansi.Strip(tinted))
	}
	if !strings.HasPrefix(tinted, bgAdd) {
		t.Fatalf("background not opened: %q", tinted)
	}
}

func TestTintLineReemitsBackgroundAfterReset(t *testing.T) {
	highlighted := "\x1b[38;5;197mfunc\x1b[0m main"
	tinted := tintLine(highlighted, nil, bgDel, bgDelSpan)
	if ansi.Strip(tinted) != "func main" {
		t.Fatalf("text mangled: %q", ansi.Strip(tinted))
	}
	resetAt := strings.Index(tinted, "\x1b[0m")
	if resetAt < 0 || !strings.HasPrefix(tinted[resetAt+4:], bgDel) {
		t.Fatalf("background must be re-opened after chroma reset: %q", tinted)
	}
}

func TestTintLineSpanSwitch(t *testing.T) {
	tinted := tintLine("if t <= exp", []diff.Span{{Start: 5, End: 7}}, bgAdd, bgAddSpan)
	if !strings.Contains(tinted, bgAddSpan) {
		t.Fatalf("span background missing: %q", tinted)
	}
	if ansi.Strip(tinted) != "if t <= exp" {
		t.Fatalf("text mangled: %q", ansi.Strip(tinted))
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
