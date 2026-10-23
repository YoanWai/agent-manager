package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func plainFill(s string, width int, _ string) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func testChrome() Chrome {
	return Chrome{Title: lipgloss.NewStyle()}
}

func TestCardWidthForKeepsRoomAtBothEdges(t *testing.T) {
	if got := CardWidthFor(120); got != 64 {
		t.Fatalf("wide terminal card width = %d, want 64", got)
	}
	if got := CardWidthFor(40); got != 36 {
		t.Fatalf("narrow terminal card width = %d, want 36", got)
	}
	if got := CardWidthFor(20); got != 64 {
		t.Fatalf("a terminal too narrow to shrink into keeps the default width, got %d", got)
	}
}

func TestCardInnerWidthDropsChromeAndPadding(t *testing.T) {
	if got := CardInnerWidth(64); got != 56 {
		t.Fatalf("inner width = %d, want 56", got)
	}
}

func TestChromeRowsCountsTheStatusBlock(t *testing.T) {
	if got := ChromeRows(1, false); got != 6 {
		t.Fatalf("chrome rows = %d, want 6", got)
	}
	if got := ChromeRows(2, true); got != 9 {
		t.Fatalf("chrome rows with a status = %d, want 9", got)
	}
}

func TestBoxFramesTheTitleAndHint(t *testing.T) {
	lines, hits := Box(Context{Width: 40, Height: 12}, testChrome(), 30, "rename", "line", [][2]string{{"↵", "save"}}, plainFill)
	if len(lines) < 6 {
		t.Fatalf("a card with a body and a hint took %d rows:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "rename") || !strings.Contains(plain, "line") || !strings.Contains(plain, "↵ save") {
		t.Fatalf("card lost its title, body or hint:\n%s", plain)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want one for the hint's key", hits)
	}
	if hits[0].Y != len(lines)-2 {
		t.Fatalf("hint hit row = %d, want the last body row %d", hits[0].Y, len(lines)-2)
	}
}

func TestPlacementCentersOnTheFrame(t *testing.T) {
	box := []string{"aaaa", "bbbb"}
	top, left, right := Placement(Context{Width: 20, Height: 10}, box)
	if top != 4 || left != 8 || right != 12 {
		t.Fatalf("placement = top %d, left %d, right %d, want 4, 8, 12", top, left, right)
	}
}
