package frame

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRowColumnsRightAlignsMeta(t *testing.T) {
	if got := RowColumns("api", "working", 20); got != "api"+strings.Repeat(" ", 10)+"working" {
		t.Fatalf("RowColumns = %q", got)
	}
	if got := RowColumns("api", "", 20); got != "api" {
		t.Fatalf("a row with no meta changed to %q", got)
	}
	if got := RowColumns("api", "working", 6); got != "api  working" {
		t.Fatalf("a narrow row should keep meta inline, got %q", got)
	}
}

func TestDisplayGroupNamesTheRoot(t *testing.T) {
	if got := DisplayGroup(""); got != "root" {
		t.Fatalf("empty group = %q, want root", got)
	}
	if got := DisplayGroup("work"); got != "work" {
		t.Fatalf("named group = %q", got)
	}
}

func TestRelTimeStepsInFivesUnderAMinute(t *testing.T) {
	now := time.Now()
	if got := RelTime(now.Add(-3 * time.Second)); got != "0s" {
		t.Fatalf("three seconds ago = %q, want 0s", got)
	}
	if got := RelSince(now.Add(-90 * time.Second)); got != "1m ago" {
		t.Fatalf("ninety seconds ago = %q", got)
	}
	if got := RelTime(now.Add(-3 * time.Hour)); got != "3h" {
		t.Fatalf("three hours ago = %q", got)
	}
	if got := RelTime(now.Add(-49 * time.Hour)); got != "2d" {
		t.Fatalf("two days ago = %q", got)
	}
}

func TestHumanAndDiskBytesPickTheirBase(t *testing.T) {
	for _, tc := range []struct {
		value uint64
		want  string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0KB"},
		{1_000_000, "1.0MB"},
		{169_850_000_000, "169.8GB"},
		{1_000_000_000_000, "1.0TB"},
	} {
		if got := DiskBytes(tc.value); got != tc.want {
			t.Errorf("DiskBytes(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
	if got := HumanBytes(1 << 20); got != "1.0MB" {
		t.Errorf("HumanBytes(1MiB) = %q, want 1.0MB", got)
	}
}

func TestShortHomeKeepsTheDistinguishingTail(t *testing.T) {
	sep := string(os.PathSeparator)
	if got := ShortHome("/home/ana"+sep+"work"+sep+"api", "/home/ana"); got != "~"+sep+"work"+sep+"api" {
		t.Fatalf("shortened path = %q", got)
	}
	if got := ShortHome("/home/ana", "/home/ana"); got != "~" {
		t.Fatalf("home itself = %q", got)
	}
	if got := ShortHome("/srv"+sep+"api", "/home/ana"); got != "/srv"+sep+"api" {
		t.Fatalf("path outside home = %q", got)
	}
}

func TestScrollWindowKeepsTheCursorVisible(t *testing.T) {
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
			start, end := ScrollWindow(tc.total, tc.cursor, tc.height)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("ScrollWindow(%d,%d,%d) = %d,%d want %d,%d",
					tc.total, tc.cursor, tc.height, start, end, tc.wantStart, tc.wantEnd)
			}
			if tc.cursor < start || tc.cursor >= end {
				t.Fatalf("cursor %d outside window [%d,%d)", tc.cursor, start, end)
			}
		})
	}
}
