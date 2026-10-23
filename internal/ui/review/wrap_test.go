package review

import (
	"strings"
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
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

func TestWrapTintedWordBoundary(t *testing.T) {
	line := "hello world foo bar"
	rows := wrapTinted(line, nil, "", "", 14)
	stripped := ansi.Strip(strings.Join(rows, ""))
	stripped = strings.TrimSpace(stripped)
	words := strings.Fields(stripped)
	if len(words) != 4 {
		t.Fatalf("expected 4 words, got %d: %q", len(words), words)
	}
	for _, w := range words {
		if !strings.Contains("hello world foo bar", w) {
			t.Fatalf("unexpected word fragment: %q", w)
		}
	}
}

func TestWrapTintedLongWordFallsBack(t *testing.T) {
	word := strings.Repeat("x", 60)
	line := "before " + word + " after"
	rows := wrapTinted(line, nil, "", "", 20)
	stripped := ansi.Strip(strings.Join(rows, ""))
	if !strings.Contains(stripped, "before") {
		t.Fatal("missing before")
	}
	if !strings.Contains(stripped, "after") {
		t.Fatal("missing after")
	}
	if xs := strings.Count(stripped, "x"); xs != 60 {
		t.Fatalf("expected 60 x's, got %d", xs)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 20 {
			t.Fatalf("row exceeds width: %d", w)
		}
	}
}

func TestWrapTintedWordWrapWithSpans(t *testing.T) {
	line := "if a > b return c"
	rows := wrapTinted(line, []diff.Span{{Start: 5, End: 6}}, bgAdd, bgAddSpan, 10)
	stripped := ansi.Strip(strings.Join(rows, ""))
	stripped = strings.ReplaceAll(stripped, " ", "")
	if stripped != "ifa>breturnc" {
		t.Fatalf("content lost or reordered: %q", stripped)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 10 {
			t.Fatalf("row exceeds width: %d row=%q", w, row)
		}
	}
}

func TestWrapTintedWordWrapNoSpaces(t *testing.T) {
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

func TestEscapeSpansFollowTheEscapedText(t *testing.T) {
	// "a\x1bbc": the span over "bc" starts at byte 2 raw, byte 3 escaped,
	// because the ESC now occupies "^[".
	spans := escapeSpans("a\x1bbc", []diff.Span{{Start: 2, End: 4}})
	if len(spans) != 1 || spans[0] != (diff.Span{Start: 3, End: 5}) {
		t.Fatalf("spans = %+v, want [{3 5}]", spans)
	}
	// "a\x9bbc": the malformed byte renders as four bytes of hex, so the span
	// over "bc" moves from byte 2 to byte 5.
	spans = escapeSpans("a\x9bbc", []diff.Span{{Start: 2, End: 4}})
	if len(spans) != 1 || spans[0] != (diff.Span{Start: 5, End: 7}) {
		t.Fatalf("malformed-byte spans = %+v, want [{5 7}]", spans)
	}

	// "a\u009bbc": the C1 rune is two bytes raw and six escaped, so the span
	// over "bc" moves from byte 3 to byte 7.
	spans = escapeSpans("a\u009bbc", []diff.Span{{Start: 3, End: 5}})
	if len(spans) != 1 || spans[0] != (diff.Span{Start: 7, End: 9}) {
		t.Fatalf("C1 spans = %+v, want [{7 9}]", spans)
	}

	clean := []diff.Span{{Start: 1, End: 2}}
	if got := escapeSpans("abc", clean); &got[0] != &clean[0] {
		t.Fatal("a line with no control bytes should keep its spans untouched")
	}
}
