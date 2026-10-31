package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func testCap() Cap {
	plain := lipgloss.NewStyle()
	return Cap{Key: plain, Label: plain, Muted: plain}
}

// The header keeps the wordmark against the left edge and the richest
// reading that leaves it room against the right, with the gutter kept past
// it.
func TestHeaderRowKeepsTheGutterPastTheReading(t *testing.T) {
	got := ansi.Strip(HeaderRow("wordmark", []string{"42 agents · 3 working", "42 agents", ""}, 40, 2))
	if !strings.HasPrefix(got, "wordmark") {
		t.Fatalf("header lost its left edge: %q", got)
	}
	if !strings.HasSuffix(got, "  ") {
		t.Fatalf("header lost its gutter: %q", got)
	}
	if !strings.Contains(got, "42 agents") || ansi.StringWidth(got) < 40 {
		t.Fatalf("header = %q, want the richest reading that fits", got)
	}
}

// A band too narrow for both readings keeps the wordmark alone rather than
// spilling past the edge.
func TestHeaderRowDropsTheReadingWhenTheBandIsNarrow(t *testing.T) {
	if got := HeaderRow("wordmark", []string{"42 agents"}, 12, 2); got != "wordmark" {
		t.Fatalf("narrow header = %q, want the wordmark alone", got)
	}
	if got := HeaderRow("wordmark", []string{""}, 80, 2); got != "wordmark" {
		t.Fatalf("header with no reading = %q, want the wordmark alone", got)
	}
}

// A card's chrome count is the rows it actually spends, so a caller that
// budgets rows for its body counts a wrapped hint the same way the card
// paints it, and a status block on top.
func TestCardChromeCountsTheHintRowsAndStatus(t *testing.T) {
	hint := [][2]string{{"enter", "save the whole thing"}, {"esc", "close"}}
	oneRow := CardChrome(64, hint[:1], testCap(), false)
	if got, want := oneRow, ChromeRows(1, false); got != want {
		t.Fatalf("chrome rows = %d, want %d", got, want)
	}
	// A card too narrow for both bindings spends a row on the second.
	wide := CardChrome(64, hint, testCap(), false)
	narrow := CardChrome(28, hint, testCap(), false)
	if narrow <= wide {
		t.Fatalf("a wrapped hint took %d rows, want more than the %d it fits in", narrow, wide)
	}
	if got, want := CardChrome(64, hint[:1], testCap(), true)-oneRow, 2; got != want {
		t.Fatalf("a status block added %d rows, want %d", got, want)
	}
}

// A flex card is never narrower than the default and never wider than the
// terminal, and it grows to fit a body line the default would clip.
func TestFlexCardWidthGrowsOnlyWhenItHasTo(t *testing.T) {
	hint := [][2]string{{"esc", "close"}}
	if got := FlexCardWidth(120, "title", "short", hint, "", testCap()); got != CardWidthFor(120) {
		t.Fatalf("flex width = %d, want the default %d", got, CardWidthFor(120))
	}
	long := strings.Repeat("x", CardWidthFor(120))
	if got := FlexCardWidth(120, "title", long, hint, "", testCap()); got <= CardWidthFor(120) {
		t.Fatalf("flex width = %d, want a card wider than the default body", got)
	}
	if got := FlexCardWidth(40, "title", strings.Repeat("x", 200), hint, "", testCap()); got != 36 {
		t.Fatalf("flex width on a 40-column terminal = %d, want 36", got)
	}
}

// The status block is part of the card, so a long one widens the card it
// would otherwise overflow.
func TestFlexCardWidthMeasuresTheStatusBlock(t *testing.T) {
	hint := [][2]string{{"esc", "close"}}
	status := strings.Repeat("cannot read the store: ", 4)
	if got := FlexCardWidth(160, "title", "short", hint, status, testCap()); got <= CardWidthFor(160) {
		t.Fatalf("flex width = %d, want a card wide enough for its status", got)
	}
}

func TestCenteredFloatsTheBoxOnTheBackdrop(t *testing.T) {
	box := []string{"╭──╮", "│ab│", "╰──╯"}
	got := Centered(Context{Width: 10, Height: 6}, box, "", plainFill)
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("frame = %d rows, want 6:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[1], "╭──╮") {
		t.Fatalf("box is not centered vertically:\n%s", got)
	}
	if !strings.Contains(lines[0], strings.Repeat(" ", 10)) {
		t.Fatalf("backdrop row is not the frame's width:\n%q", lines[0])
	}
}
