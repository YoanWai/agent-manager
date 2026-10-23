package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/x/ansi"
)

func TestStartingSessionGlyphMovesOnTheStartupTick(t *testing.T) {
	for _, tool := range []string{"agent", "shell"} {
		m := previewModel(status.Starting, blankCapture)
		selected := railSelectedSession(m)
		setRailSessionTool(m, selected.ID, tool)
		m.services.cfg.Tools = map[string]config.Tool{"shell": {Shell: true}}
		first := ansi.Strip(m.sessionGlyph(railSelectedSession(m)))
		m.Update(startupTickMsg{})
		second := ansi.Strip(m.sessionGlyph(railSelectedSession(m)))
		if first == second {
			t.Fatalf("starting %s glyph stayed on %q", tool, first)
		}
		if second == m.glyphs().ShellGlyph {
			t.Fatal("starting shell used the resting shell glyph")
		}
	}
}

func TestBootShowsFullScreenRing(t *testing.T) {
	m := shotModel()
	m.startup.booting = true
	body := ansi.Strip(preparedView(m))
	if strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("boot should hide the list:\n%s", body)
	}
	if !strings.Contains(body, "loading") {
		t.Fatalf("boot should be the preview ring, got:\n%s", body)
	}
	m.startup.booting = false
	body = ansi.Strip(preparedView(m))
	if !strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("after boot the list should show:\n%s", body)
	}
}
