package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"

	"github.com/charmbracelet/x/ansi"
)

// A settled wordmark keeps a timer alive for the next shimmer, and the
// shimmer message starts the sweep over.
func TestBannerShimmerReArms(t *testing.T) {
	m := &Model{
		startup: startupState{
			BannerPhase: uistartup.Frames,
		},
	}
	if m.bannerTick() == nil {
		t.Fatal("a settled banner should wait for the next shimmer, not stop")
	}
	updated, cmd := m.Update(uistartup.BannerShimmerMsg{})
	m = updated.(*Model)
	if m.startup.BannerPhase != 0 {
		t.Fatalf("shimmer should restart the sweep, phase = %d", m.startup.BannerPhase)
	}
	if cmd == nil {
		t.Fatal("a restarted sweep must schedule its frames")
	}
}

func TestStartingSessionGlyphMovesOnTheStartupTick(t *testing.T) {
	for _, tool := range []string{"agent", "shell"} {
		m := previewModel(status.Starting, blankCapture)
		selected := railSelectedSession(m)
		setRailSessionTool(m, selected.ID, tool)
		m.services.cfg.Tools = map[string]config.Tool{"shell": {Shell: true}}
		first := ansi.Strip(m.sessionGlyph(railSelectedSession(m)))
		m.Update(uistartup.TickMsg{})
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
	m.startup.Booting = true
	body := ansi.Strip(preparedView(m))
	if strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("boot should hide the list:\n%s", body)
	}
	if !strings.Contains(body, "loading") {
		t.Fatalf("boot should be the preview ring, got:\n%s", body)
	}
	m.startup.Booting = false
	body = ansi.Strip(preparedView(m))
	if !strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("after boot the list should show:\n%s", body)
	}
}
