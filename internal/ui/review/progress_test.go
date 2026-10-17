package review_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/ui/review"
)

func TestExcerptKeepsRuneBoundary(t *testing.T) {
	line := "  " + strings.Repeat("ש", 70)
	excerpt := review.Excerpt(line)
	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt split a rune: %q", excerpt)
	}
	if got := len([]rune(excerpt)); got != 60 {
		t.Fatalf("excerpt rune count = %d, want 60", got)
	}
	if short := review.Excerpt("  short  "); short != "short" {
		t.Fatalf("short excerpt = %q", short)
	}
}
