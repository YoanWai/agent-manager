package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
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
