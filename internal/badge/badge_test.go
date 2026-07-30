package badge

import (
	"strings"
	"testing"
)

func TestSVGCarriesValueAndFitsText(t *testing.T) {
	chip := Chip{Label: "stars", Value: "164", Color: "#d08442", Icon: "star"}
	svg := chip.SVG(Dark)

	for _, want := range []string{`aria-label="stars: 164"`, ">stars<", ">164<", Dark.Surface, "#d08442"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("svg missing %q:\n%s", want, svg)
		}
	}
	if !strings.Contains(svg, `lengthAdjust="spacing"`) {
		t.Fatal("text run is not pinned to the width the chip reserved for it")
	}
	if strings.Contains(svg, "spacingAndGlyphs") {
		t.Fatal("glyphs are being distorted to fit the box")
	}
}

func TestIconWidensTheChip(t *testing.T) {
	bare := Chip{Label: "go", Value: "1.26+"}
	withIcon := bare
	withIcon.Icon = "star"
	if withIcon.Width() <= bare.Width() {
		t.Fatalf("icon chip %.1f is not wider than bare chip %.1f", withIcon.Width(), bare.Width())
	}
}

func TestUnknownIconLeavesNoGap(t *testing.T) {
	chip := Chip{Label: "platform", Value: "macOS", Icon: "nope"}
	if strings.Contains(chip.SVG(Light), "<path") {
		t.Fatal("unknown icon still drew a path")
	}
	bare := Chip{Label: "platform", Value: "macOS"}
	if chip.Width() != bare.Width() {
		t.Fatal("unknown icon still reserved width")
	}
}

func TestEscapesMarkup(t *testing.T) {
	chip := Chip{Label: `a"<b`, Value: "x&y"}
	svg := chip.SVG(Light)
	if strings.Contains(svg, `a"<b`) || strings.Contains(svg, "x&y") {
		t.Fatalf("raw markup survived escaping:\n%s", svg)
	}
}
