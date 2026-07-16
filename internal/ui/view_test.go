package ui

import (
	"strings"
	"testing"
)

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
