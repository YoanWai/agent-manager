package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestPendingActionMarksMatchHelpInBothModes(t *testing.T) {
	for _, tc := range []struct {
		mode          presentation.GlyphMode
		archive, kill string
	}{{presentation.GlyphModeBasic, "A", "K"}, {presentation.GlyphModeAdvanced, "↓", "■"}} {
		m := shotModel()
		m.prefs.glyphMode = tc.mode
		for action, want := range map[string]string{store.AfterTurnArchive: tc.archive, store.AfterTurnKill: tc.kill} {
			if got := ansi.Strip(m.afterTurnBadge(action)); got != want {
				t.Fatalf("%s/%s badge=%q", tc.mode, action, got)
			}
		}
		found := false
		m.openHelp()
		m.layout.width, m.layout.height = 120, 100
		found = strings.Contains(ansi.Strip(m.viewHelp()), tc.archive+" / "+tc.kill)
		if !found {
			t.Fatalf("%s help has different lifecycle marks", tc.mode)
		}
	}
}

func TestCompatibleGaugeRoundsWithoutChangingRampOrWidth(t *testing.T) {
	forceANSI256(t)
	for _, tc := range []struct {
		percent float64
		filled  int
	}{{-1, 0}, {0, 0}, {4.9, 0}, {5, 1}, {14.9, 1}, {15, 2}, {95, 10}, {100, 10}, {101, 10}} {
		for _, invert := range []bool{false, true} {
			got := gauge(tc.percent, 10, invert, presentation.GlyphModeBasic)
			if ansi.Strip(got) != strings.Repeat("─", 10) {
				t.Fatalf("compatible gauge: %q", got)
			}
			_, colors := cellColors(got, 10, 1)
			_, advanced := cellColors(gauge(tc.percent, 10, invert, presentation.GlyphModeAdvanced), 10, 1)
			_, empty := cellColors(gauge(0, 10, false, presentation.GlyphModeBasic), 10, 1)
			filled := 0
			for i, color := range colors[0] {
				if !sameColor(color, empty[0][i]) {
					filled++
				}
			}
			if filled != tc.filled {
				t.Fatalf("%.1f%% invert=%t: %d lit cells, want %d", tc.percent, invert, filled, tc.filled)
			}
			if tc.filled > 0 && !sameColor(colors[0][0], advanced[0][0]) {
				t.Fatal("compatible gauge changed its ramp")
			}
		}
	}
	for _, width := range []int{0, 1, 4, 10} {
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			if got := ansi.StringWidth(gauge(50, width, false, mode)); got != max(width, 1) {
				t.Fatalf("gauge width=%d want=%d", got, max(width, 1))
			}
		}
	}
	if got := ansi.Strip(gauge(5, 10, false, presentation.GlyphModeAdvanced)); got != "╸"+strings.Repeat("━", 9) {
		t.Fatalf("Advanced cap changed: %q", got)
	}
}
