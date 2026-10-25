package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleTopEdgeKeepsFrameGeometry(t *testing.T) {
	m := shotModel()
	for _, width := range []int{0, 1, 34, 80, 120} {
		m.prefs.glyphMode = presentation.GlyphModeBasic
		for _, paneWidth := range []int{1, width / 3, width} {
			got := ansi.Strip(m.railTopRow(paneWidth, width))
			if got != strings.Repeat("─", width) {
				t.Fatalf("%d/%d top edge=%q", width, paneWidth, got)
			}
		}
	}
	for _, full := range []bool{false, true} {
		for _, hideHeader := range []bool{false, true} {
			for _, width := range []int{34, 80, 120} {
				m.layout.width, m.layout.height = width, 34
				m.prefs.fullLayout, m.prefs.hideHeader = full, hideHeader
				for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
					m.prefs.glyphMode = mode
					frame := ansi.Strip(preparedView(m))
					rows := strings.Split(frame, "\n")
					if len(rows) != 34 {
						t.Fatalf("%s/%d/%t height=%d", mode, width, full, len(rows))
					}
					for _, row := range rows {
						if ansi.StringWidth(row) != width {
							t.Fatalf("%s/%d row=%q", mode, width, row)
						}
					}
					if mode == presentation.GlyphModeBasic && strings.ContainsAny(frame, "🬹🬂🬨") {
						t.Fatal("compatible frame contains sextants")
					}
				}
			}
		}
	}
	m.prefs.glyphMode = presentation.GlyphModeAdvanced
	if got := ansi.Strip(m.railTopRow(30, 80)); !strings.HasPrefix(got, "🬹"+strings.Repeat("🬂", 29)+"🬨") {
		t.Fatalf("Advanced edge changed: %q", got)
	}
}
