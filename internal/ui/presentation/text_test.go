package presentation

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestPadRightUsesDisplayWidthAndKeepsStyledRowsClosed(t *testing.T) {
	styled := "\x1b[1m界\x1b[0m"
	got := PadRight(styled, 4)
	if width := ansi.StringWidth(got); width != 4 {
		t.Fatalf("display width = %d, want 4: %q", width, got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("styled padded row did not close its SGR state: %q", got)
	}
	if got := ansi.Strip(PadRight("abcdef", 4)); got != "abc…" {
		t.Fatalf("truncated row = %q, want %q", got, "abc…")
	}
}

func TestDividerUsesCallerStylesAndExactWidth(t *testing.T) {
	section := lipgloss.NewStyle().Bold(true)
	rule := lipgloss.NewStyle().Underline(true)
	got := Divider("list", 20, section, rule)
	if width := ansi.StringWidth(got); width != 20 {
		t.Fatalf("divider width = %d, want 20: %q", width, got)
	}
	if plain := ansi.Strip(got); !strings.HasPrefix(plain, "▍list ") {
		t.Fatalf("divider = %q", plain)
	}
}

func TestWindowClampsAndMarksHiddenContent(t *testing.T) {
	lines := []string{"zero", "one", "two", "three", "four"}
	style := lipgloss.NewStyle().Bold(true)

	top := Window(lines, 3, -10, style)
	if got := ansi.Strip(strings.Join(top, "\n")); got != "zero\none\n↓ more below…" {
		t.Fatalf("top window:\n%s", got)
	}

	bottom := Window(lines, 3, 99, style)
	if got := ansi.Strip(strings.Join(bottom, "\n")); got != "↑ more above…\nthree\nfour" {
		t.Fatalf("bottom window:\n%s", got)
	}

	middle := Window(lines, 1, 2, style)
	if got := ansi.Strip(strings.Join(middle, "\n")); got != "↕ more…" {
		t.Fatalf("one-row window = %q", got)
	}
}

func TestTruncateTailRuneSafe(t *testing.T) {
	hebrew := "/home/dev/פרויקטים/agent-manager"
	tail := TruncateTail(hebrew, 10)
	if !strings.HasPrefix(tail, "…") || len([]rune(tail)) != 10 {
		t.Fatalf("TruncateTail broken: %q (%d runes)", tail, len([]rune(tail)))
	}
	if TruncateTail("short", 10) != "short" {
		t.Fatal("short strings should pass through")
	}
	if got := TruncateTail("abcdef", 1); got != "…" {
		t.Fatalf("max 1 = %q, want ellipsis", got)
	}
	if got := TruncateTail("abcdef", 0); got != "" {
		t.Fatalf("max 0 = %q, want empty", got)
	}
}

func TestTruncatePathCutsAtSeparator(t *testing.T) {
	path := "internal/api/handlers/sessions.go"
	got := TruncatePath(path, 16)
	if !strings.HasPrefix(got, "…/") || !strings.HasSuffix(got, "sessions.go") {
		t.Fatalf("TruncatePath = %q, want a slash-cut tail ending in sessions.go", got)
	}
	if strings.Contains(got, "ternal") {
		t.Fatalf("cut mid-segment: %q", got)
	}
	if TruncatePath("short.go", 20) != "short.go" {
		t.Fatal("short paths should pass through")
	}
	if got := TruncatePath(path, 1); got != "…" {
		t.Fatalf("limit 1 = %q, want ellipsis", got)
	}
	if got := TruncatePath(path, 0); got != "" {
		t.Fatalf("limit 0 = %q, want empty", got)
	}
}
