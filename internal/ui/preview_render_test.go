package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
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

func TestPaneExactPreservesBlanks(t *testing.T) {
	pane := "one\n\n\ntwo\n"
	got := paneExact(pane, 10, 80, -1)
	if len(got) != 4 || got[1] != "" || got[2] != "" {
		t.Fatalf("paneExact should keep blank rows: %q", got)
	}
	got = paneExact("a\nb\nc\nd", 2, 80, -1)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("paneExact oversize should take bottom lines: %q", got)
	}
}

// A pane held taller than the panel (kept tall on purpose, since a height
// shrink costs Codex its scrollback, #369) carries a blank tail under
// short output. The crop returns only its content so the renderer can
// align it without carrying the dead rows along.
func TestPaneWindowCropsBlankTailNotContent(t *testing.T) {
	tall := "one\ntwo" + strings.Repeat("\n", 40)
	lines, start := paneWindow(tall, 5, -1)
	if start != 0 || len(lines) != 2 || lines[0] != "one" || lines[1] != "two" {
		t.Fatalf("short output in a tall pane = start %d rows %q, want content only", start, lines)
	}

	full := strings.TrimSuffix(strings.Repeat("line\n", 40), "\n")
	lines, start = paneWindow(full, 5, -1)
	if start != 35 || len(lines) != 5 {
		t.Fatalf("full pane = start %d, %d rows, want the bottom five", start, len(lines))
	}

	mid := "head" + strings.Repeat("\nbody", 30) + strings.Repeat("\n", 9)
	lines, start = paneWindow(mid, 5, -1)
	if start != 26 || lines[len(lines)-1] != "body" {
		t.Fatalf("mid-ending content = start %d last %q, want the crop to end on it", start, lines[len(lines)-1])
	}
}

// The caret can rest below the last painted row, on an empty prompt line,
// and the crop must keep its row on screen.
func TestPaneWindowKeepsCaretRow(t *testing.T) {
	tall := "one" + strings.Repeat("\n", 40)
	lines, start := paneWindow(tall, 5, 20)
	if start != 16 || len(lines) != 5 {
		t.Fatalf("caret at row 20 = start %d, %d rows, want rows 16..20", start, len(lines))
	}
}

func TestPreviewLine(t *testing.T) {
	colored := "\x1b[38;5;42mgreen text\x1b[39m"
	got := previewLine(colored, 80)
	if !strings.Contains(got, "\x1b[38;5;42m") {
		t.Fatalf("color escapes should survive: %q", got)
	}
	if !strings.Contains(got, "\x1b[0m") {
		t.Fatalf("line with ANSI must reset SGR: %q", got)
	}
	erased := "abc\x1b[K\x1b[2Jdef"
	if got := ansi.Strip(previewLine(erased, 80)); strings.TrimRight(got, " ") != "abcdef" {
		t.Fatalf("erase sequences should be stripped: %q", got)
	}
	scrolled := "a\x1b[1Sb\x1bMc\x1b[2Td"
	if got := ansi.Strip(previewLine(scrolled, 80)); strings.TrimRight(got, " ") != "abcd" {
		t.Fatalf("scroll sequences should be stripped: %q", got)
	}

	control := "a\rb\bc"
	if got := ansi.Strip(previewLine(control, 80)); strings.TrimRight(got, " ") != "abc" {
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
	if w := ansi.StringWidth(plain); w != 80 {
		t.Fatalf("pane row should fill its column, width %d", w)
	}
	mixed := ansi.Strip(previewLine("קודינג\",\"slowly\":false", 80))
	if strings.TrimRight(mixed, " ") != "קודינג\",\"slowly\":false" {
		t.Fatalf("mixed-direction pane row lost its text: %q", mixed)
	}
	// An RTL row is painted like any other row: the same cells and no
	// direction marks, so every host is handed the same frame.
	hebrew := previewLine("  העצמון 25 חולון", 40)
	if strings.ContainsAny(hebrew, "\u200e\u2066\u2069") {
		t.Fatalf("pane row must carry no bidi controls: %q", hebrew)
	}
	if w := ansi.StringWidth(hebrew); w != 40 {
		t.Fatalf("RTL row must pad to column width, got %d", w)
	}
	if body := strings.TrimRight(ansi.Strip(hebrew), " "); body != "  העצמון 25 חולון" {
		t.Fatalf("RTL row text changed: %q", body)
	}
}

// A row `go test` wrote carries literal tabs, which the frame measures as
// nothing and the host paints out to its own stops, pushing the preview,
// footer and stat rows off their positions.
func TestPaneExactExpandsTabs(t *testing.T) {
	const width = 60
	rows := paneExact("ok  \tgithub.com/YoanWai/agent-manager/internal/ui\t73.163s\n", 4, width, -1)
	if len(rows) != 1 || strings.ContainsRune(rows[0], '\t') {
		t.Fatalf("tab reached the frame: %q", rows)
	}
	if !strings.HasPrefix(rows[0], "ok      github.com/") {
		t.Fatalf("tab should reach the pane's next eight-column stop: %q", rows[0])
	}
	if w := ansi.StringWidth(previewLine(rows[0], width)); w != width {
		t.Fatalf("tabbed row paints %d cells, want %d", w, width)
	}

	// A tab close to the right edge stops on the row's last cell, where
	// tmux leaves it, so the cell painted after it stays in the preview.
	// Measured on tmux 3.7b: in a 62-column pane, 58 columns then a tab
	// leaves the cursor at column 61.
	edge := paneExact(strings.Repeat("a", 58)+"\tX", 1, 62, -1)
	if want := strings.Repeat("a", 58) + "   X"; edge[0] != want {
		t.Fatalf("tab at the edge expanded to %q, want %q", edge[0], want)
	}

	// A capture taken before a resize is wider than the box it lands in,
	// and a tab past the right edge covers no cells at all.
	stale := paneExact(strings.Repeat("a", 40)+"\tX", 1, 20, -1)
	if want := strings.Repeat("a", 40) + "X"; stale[0] != want {
		t.Fatalf("tab past the edge expanded to %q, want %q", stale[0], want)
	}
}

// The capture keeps the agent's own colors on every theme: the pane is
// painted the theme's backdrop, so nothing has to be recolored on the way in.
func TestPreviewLineKeepsAgentColors(t *testing.T) {
	for _, name := range []string{"classic", "solarized light"} {
		applyTheme(themes[themeIndex(name)])
		colored := "\x1b[38;5;42mhi\x1b[39m"
		got := previewLine(colored, 4)
		if !strings.Contains(got, "\x1b[38;5;42m") {
			t.Errorf("%s: agent color dropped: %q", name, got)
		}
		if body := strings.TrimRight(ansi.Strip(got), " "); body != "hi" {
			t.Errorf("%s: preview line rewritten: %q", name, got)
		}
	}
	applyTheme(themes[0])
}

func TestTruncateRuneSafe(t *testing.T) {
	hebrew := "/home/dev/פרויקטים/agent-manager"
	tail := truncateTail(hebrew, 10)
	if !strings.HasPrefix(tail, "…") || len([]rune(tail)) != 10 {
		t.Fatalf("truncateTail broken: %q (%d runes)", tail, len([]rune(tail)))
	}
	if truncateTail("short", 10) != "short" {
		t.Fatal("short strings should pass through")
	}
	if got := truncateTail("abcdef", 1); got != "…" {
		t.Fatalf("max 1 = %q, want ellipsis", got)
	}
	if got := truncateTail("abcdef", 0); got != "" {
		t.Fatalf("max 0 = %q, want empty", got)
	}
}

func TestTruncatePathCutsAtSeparator(t *testing.T) {
	path := "internal/api/handlers/sessions.go"
	got := truncatePath(path, 16)
	if !strings.HasPrefix(got, "…/") || !strings.HasSuffix(got, "sessions.go") {
		t.Fatalf("truncatePath = %q, want a slash-cut tail ending in sessions.go", got)
	}
	if strings.Contains(got, "ternal") {
		t.Fatalf("cut mid-segment: %q", got)
	}
	if truncatePath("short.go", 20) != "short.go" {
		t.Fatal("short paths should pass through")
	}
	if got := truncatePath(path, 1); got != "…" {
		t.Fatalf("limit 1 = %q, want ellipsis", got)
	}
	if got := truncatePath(path, 0); got != "" {
		t.Fatalf("limit 0 = %q, want empty", got)
	}
}
