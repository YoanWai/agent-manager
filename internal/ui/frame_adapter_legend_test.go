package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestViewLegendPaintsWaitingMessagesWarm(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	waiting := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		release:  uirelease.State{Latest: "v0.41.0", Version: "0.40.0"},
	}
	bar := paintLegend([]legendSection{waiting.viewLegend()}, 160).text
	if !strings.Contains(bar, uitheme.LegendAlertStyle().Render("M unread")) {
		t.Fatalf("waiting messages should use the working color:\n%s", bar)
	}
	idle := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		release:  uirelease.State{Version: "0.40.0"},
		notices:  noticesPanel{Panel: uinotices.NewPanel(map[string]bool{uinotices.Welcome: true, uinotices.ArrowStep: true})},
	}
	idleBar := ansi.Strip(paintLegend([]legendSection{idle.viewLegend()}, 160).text)
	if strings.Contains(idleBar, "unread") {
		t.Fatalf("an empty panel should leave the messages key off:\n%s", idleBar)
	}
}

// The root's tier literals reach the painter: every field the row and view
// legends set survives the copy into ui/frame.
func TestRootLegendKeepsItsTiersAndHits(t *testing.T) {
	sections := []legendSection{{
		title:  "Session",
		leads:  [][2]string{{"p", "quick prompt mode"}},
		alerts: [][2]string{{"M", "unread"}},
		hint:   "a hint",
		pairs:  [][2]string{{"r", "rename"}},
	}}
	painted := paintLegend(sections, 200)
	plain := ansi.Strip(painted.text)
	for _, want := range []string{"Session", "p quick prompt mode", "M unread", "a hint", "r rename"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("root legend lost %q:\n%s", want, plain)
		}
	}
	if len(painted.hits) != 3 {
		t.Fatalf("hits = %+v, want one per pressable pair", painted.hits)
	}
	lines := strings.Split(plain, "\n")
	for _, hit := range painted.hits {
		if hit.y < 0 || hit.y >= len(lines) {
			t.Fatalf("hit %+v lands on no painted row", hit)
		}
	}
}
