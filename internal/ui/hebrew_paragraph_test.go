package ui

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Pure-RTL pane rows that sit beside empty rail cells must still open with a
// strong LTR mark before any Hebrew. Hosts that pick paragraph direction from
// the first strong character otherwise right-justify the whole terminal line
// and the pane text lands in the rail.
func TestHebrewPaneRowKeepsLTRParagraph(t *testing.T) {
	he := strings.Join([]string{
		"english line",
		"  העצמון 25 חולון",
		"mixed ת.ז. end",
		"",
		"  העצמון 25 חולון",
	}, "\n")
	// Tall frames put pure-Hebrew pane rows next to empty rail padding, which
	// is the geometry that flips paragraph direction without a leading LRM.
	for _, width := range []int{100, 140, 180} {
		for _, height := range []int{40, 50} {
			foundHebrew := false
			m := shotModel()
			m.width, m.height = width, height
			m.preview = he + "\n" + strings.Repeat("  העצמון 25 חולון\n", 20)
			m.mode = modeFocus
			for i, line := range strings.Split(m.View(), "\n") {
				plain := ansi.Strip(line)
				heb := strings.IndexFunc(plain, func(r rune) bool {
					return unicode.In(r, unicode.Hebrew)
				})
				if heb < 0 {
					continue
				}
				foundHebrew = true
				lrm := strings.IndexRune(plain, '\u200e')
				if lrm < 0 || lrm > heb {
					t.Errorf("%dx%d line %d: LRM missing or after Hebrew (lrm=%d heb=%d)\n%q",
						width, height, i, lrm, heb, plain)
				}
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d line %d overflows at %d", width, height, i, got)
				}
			}
			if !foundHebrew {
				t.Fatalf("%dx%d frame did not render a Hebrew preview row", width, height)
			}
		}
	}
}
