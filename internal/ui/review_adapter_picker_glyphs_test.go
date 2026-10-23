package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/charmbracelet/x/ansi"
)

func TestRetainedRepoPickerRendersCurrentMode(t *testing.T) {
	m := shotModel()
	const name = "❯⌥界e\u0301"
	for _, tc := range []struct {
		mode          presentation.GlyphMode
		title, cursor string
	}{{presentation.GlyphModeBasic, "D", ">"}, {presentation.GlyphModeAdvanced, "⌥", "❯"}} {
		m.prefs.glyphMode = tc.mode
		m.openReviewPicker([]uireview.PickerRow{{Label: name, Root: "/repo"}}, "Review repo", uireview.PickRepo, "", uireview.PickerSource{}, "")
		got := ansi.Strip(m.viewPicker(t))
		m.pop()
		if !strings.Contains(got, tc.title+" Review repo") || !strings.Contains(got, tc.cursor+" "+name) {
			t.Fatalf("%s picker=%q", tc.mode, got)
		}
	}
}
