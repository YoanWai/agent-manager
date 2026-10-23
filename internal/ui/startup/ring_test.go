package startup

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestRingWrapsThePhaseRoundTheRing(t *testing.T) {
	const width, height = 30, 6
	for _, phase := range []int{RingPoints, RingPoints + 3, RingPoints * 4} {
		want := Ring(presentation.GlyphsFor(""), width, height, "starting up", phase%RingPoints, testTheme())
		if got := Ring(presentation.GlyphsFor(""), width, height, "starting up", phase, testTheme()); !slices.Equal(got, want) {
			t.Fatalf("phase %d rendered\n%s\nwant the phase %d ring\n%s",
				phase, strings.Join(got, "\n"), phase%RingPoints, strings.Join(want, "\n"))
		}
	}
	lit := Ring(presentation.GlyphsFor(""), width, height, "starting up", 0, testTheme())
	if slices.Equal(lit, Ring(presentation.GlyphsFor(""), width, height, "starting up", 1, testTheme())) {
		t.Fatal("neighbouring phases render the same ring, so the comparison proves nothing")
	}

	const label = "reviving"
	rendered := ansi.Strip(strings.Join(Ring(presentation.GlyphsFor(""), width, height, label, 0, testTheme()), "\n"))
	if !strings.Contains(rendered, label) {
		t.Fatalf("the ring dropped the label it was given:\n%s", rendered)
	}
}

// The ring is cut to a panel shorter than it, and centered in a taller one.
func TestRingFitsItsPanel(t *testing.T) {
	if got := Ring(presentation.GlyphsFor(""), 40, 3, "loading", 0, testTheme()); len(got) != 3 {
		t.Fatalf("ring in a three-row panel = %d rows, want 3", len(got))
	}
	tall := Ring(presentation.GlyphsFor(""), 40, 10, "loading", 0, testTheme())
	if len(tall) != 10 {
		t.Fatalf("ring in a ten-row panel = %d rows, want 10", len(tall))
	}
	if tall[0] != "" || tall[len(tall)-1] != "" {
		t.Fatalf("ring should be centered, not pinned to the top:\n%s", strings.Join(tall, "\n"))
	}
}
