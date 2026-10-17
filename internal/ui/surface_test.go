package ui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPaintPadsCellsLeftByWideCharacterTruncation(t *testing.T) {
	for _, text := range []string{"界界界", "e\u0301界界", "x 👩‍💻 z", "\x1b[31m界界界\x1b[0m"} {
		for _, bg := range []string{"", "#112233"} {
			for width := 0; width <= 8; width++ {
				t.Run(fmt.Sprintf("%q/bg=%s/width=%d", text, bg, width), func(t *testing.T) {
					got := paint(text, width, bg)
					if cells := ansi.StringWidth(got); cells != width {
						t.Fatalf("paint uses %d cells, want %d: %q", cells, width, got)
					}
				})
			}
		}
	}
}
