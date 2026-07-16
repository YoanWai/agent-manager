package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func TestScrollWindow(t *testing.T) {
	cases := []struct {
		name                  string
		total, cursor, height int
		wantStart, wantEnd    int
	}{
		{"fits entirely", 5, 2, 10, 0, 5},
		{"cursor at top", 100, 0, 20, 0, 18},
		{"cursor centered", 100, 50, 20, 41, 59},
		{"cursor at bottom", 100, 99, 20, 82, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := scrollWindow(tc.total, tc.cursor, tc.height)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("scrollWindow(%d,%d,%d) = %d,%d want %d,%d",
					tc.total, tc.cursor, tc.height, start, end, tc.wantStart, tc.wantEnd)
			}
			if tc.cursor < start || tc.cursor >= end {
				t.Fatalf("cursor %d outside window [%d,%d)", tc.cursor, start, end)
			}
		})
	}
}

func TestPaneTail(t *testing.T) {
	pane := "one\ntwo\nthree\n\n\n"
	got := paneTail(pane, 2)
	if len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("paneTail = %v", got)
	}
	if paneTail("", 5) != nil {
		t.Fatal("empty pane should return nil")
	}
	if paneTail("x", 0) != nil {
		t.Fatal("zero budget should return nil")
	}
	ansiBlank := "real\n\x1b[38;5;42m   \x1b[0m\n"
	got = paneTail(ansiBlank, 5)
	if len(got) != 1 || got[0] != "real" {
		t.Fatalf("ANSI-only blank lines should be trimmed, got %v", got)
	}
	sparse := "top\n\n\n\n\nmiddle\n\n\n\nbottom\n\n\n"
	got = paneTail(sparse, 10)
	want := []string{"top", "", "middle", "", "bottom"}
	if len(got) != len(want) {
		t.Fatalf("blank runs should collapse to one: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blank runs should collapse to one: %q", got)
		}
	}
}

func TestPreviewLine(t *testing.T) {
	colored := "\x1b[38;5;42mgreen text\x1b[39m"
	got := previewLine(colored, 80)
	if !strings.Contains(got, "\x1b[38;5;42m") {
		t.Fatalf("color escapes should survive: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("line with ANSI must end in SGR reset: %q", got)
	}

	erased := "abc\x1b[K\x1b[2Jdef"
	if got := previewLine(erased, 80); got != "abcdef" {
		t.Fatalf("erase sequences should be stripped: %q", got)
	}

	control := "a\rb\bc"
	if got := previewLine(control, 80); got != "abc" {
		t.Fatalf("control chars should be dropped: %q", got)
	}

	wide := "\x1b[31m" + strings.Repeat("x", 100) + "\x1b[0m"
	clipped := previewLine(wide, 20)
	if w := lipglossWidth(clipped); w > 20 {
		t.Fatalf("clipped ANSI line renders %d cells, want <= 20", w)
	}

	plain := previewLine("plain", 80)
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain line should gain no escapes: %q", plain)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	hebrew := "/home/dev/פרויקטים/agent-manager"
	tail := truncateTail(hebrew, 10)
	if !strings.HasPrefix(tail, "…") || len([]rune(tail)) != 10 {
		t.Fatalf("truncateTail broken: %q (%d runes)", tail, len([]rune(tail)))
	}
	clipped := clipLine(hebrew, 10)
	if !strings.HasSuffix(clipped, "…") || len([]rune(clipped)) != 10 {
		t.Fatalf("clipLine broken: %q (%d runes)", clipped, len([]rune(clipped)))
	}
	if truncateTail("short", 10) != "short" || clipLine("short", 10) != "short" {
		t.Fatal("short strings should pass through")
	}
}
