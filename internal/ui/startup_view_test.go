package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"
)

func TestStartingSessionGlyphMovesOnTheStartupTick(t *testing.T) {
	for _, tool := range []string{"agent", "shell"} {
		m := previewModel(status.Starting, blankCapture)
		m.rail.rows[0].sess.Tool = tool
		m.services.cfg.Tools = map[string]config.Tool{"shell": {Shell: true}}
		first := ansi.Strip(m.sessionGlyph(m.rail.rows[0].sess))
		m.Update(startupTickMsg{})
		second := ansi.Strip(m.sessionGlyph(m.rail.rows[0].sess))
		if first == second {
			t.Fatalf("starting %s glyph stayed on %q", tool, first)
		}
		if second == shellGlyph {
			t.Fatal("starting shell used the resting shell glyph")
		}
	}
}

func TestRingLoaderWrapsThePhaseRoundTheRing(t *testing.T) {
	const width, height = 30, 6
	for _, phase := range []int{startupRingPoints, startupRingPoints + 3, startupRingPoints * 4} {
		want := ringLoader(width, height, "starting up", phase%startupRingPoints)
		if got := ringLoader(width, height, "starting up", phase); !slices.Equal(got, want) {
			t.Fatalf("phase %d rendered\n%s\nwant the phase %d ring\n%s",
				phase, strings.Join(got, "\n"), phase%startupRingPoints, strings.Join(want, "\n"))
		}
	}
	lit := ringLoader(width, height, "starting up", 0)
	if slices.Equal(lit, ringLoader(width, height, "starting up", 1)) {
		t.Fatal("neighbouring phases render the same ring, so the comparison proves nothing")
	}

	const label = "reviving"
	rendered := ansi.Strip(strings.Join(ringLoader(width, height, label, 0), "\n"))
	if !strings.Contains(rendered, label) {
		t.Fatalf("the ring dropped the label it was given:\n%s", rendered)
	}
}

func TestBootShowsFullScreenRing(t *testing.T) {
	m := shotModel()
	m.startup.booting = true
	body := ansi.Strip(m.View())
	if strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("boot should hide the list:\n%s", body)
	}
	if !strings.Contains(body, "loading") {
		t.Fatalf("boot should be the preview ring, got:\n%s", body)
	}
	m.startup.booting = false
	body = ansi.Strip(m.View())
	if !strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("after boot the list should show:\n%s", body)
	}
}
