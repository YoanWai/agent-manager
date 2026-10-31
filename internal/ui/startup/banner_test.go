package startup

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func testTheme() theme.Theme { return theme.Palettes[0] }

// A settled wordmark spans the mark's gradient: the two ends must differ,
// darker teal on the left.
func TestRestGradient(t *testing.T) {
	left := Rest(0, testTheme())
	right := Rest(float64(Width()-1), testTheme())
	if left == right {
		t.Fatal("gradient ends should differ")
	}
}

// The sweep lights the column under it and dims the ones it has not reached.
func TestTintLitAtTheHeadAndDimmedAhead(t *testing.T) {
	th := testTheme()
	if got, want := Tint(10, 10, th), theme.Mix(Rest(10, th), th.Bright, 1); got != want {
		t.Fatalf("head column = %q, want the brightest tone %q", got, want)
	}
	if got, want := Tint(30, 10, th), theme.Mix(Rest(30, th), th.Subtle, 0.5); got != want {
		t.Fatalf("column ahead of the sweep = %q, want the dimmed tone %q", got, want)
	}
}

// A terminal too narrow for the tracking still leads the rail with the name,
// one row, on the theme's accent.
func TestLinePlainlyWhenTheTrackingDoesNotFit(t *testing.T) {
	rows := Line(0, 2, false, testTheme())
	if len(rows) != 1 {
		t.Fatalf("wordmark = %d rows, want one header row", len(rows))
	}
	if got := ansi.Strip(rows[0]); got != "  "+Word {
		t.Fatalf("plain wordmark = %q, want the name indented by the gutter", got)
	}
}

// The tracked wordmark is one row of capital letters under the gutter, and a
// narrow terminal loses it altogether.
func TestLineTracksWhenItFits(t *testing.T) {
	rows := Line(0, 2, true, testTheme())
	if len(rows) != 1 {
		t.Fatalf("wordmark = %d rows, want one header row", len(rows))
	}
	tracked := make([]string, 0, len(Word)*2)
	for _, letter := range strings.ToUpper(Word) {
		tracked = append(tracked, string(letter))
	}
	if got, want := ansi.Strip(rows[0]), "  "+strings.Join(tracked, " "); got != want {
		t.Fatalf("tracked wordmark = %q, want %q", got, want)
	}
	if Fits(Width()+41, 2) {
		t.Fatal("a terminal without room beside the fleet readings kept the tracking")
	}
	if !Fits(Width()+42, 2) {
		t.Fatal("a terminal with room beside the fleet readings lost the tracking")
	}
}
