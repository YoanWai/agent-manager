package ui

import (
	"testing"
)

// A session open full screen owns the whole body, so the frame paints no
// rail line and leaves no hit behind for a click to resolve against.
func TestFullFocusFrameRecordsNoRailHits(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)
	if len(m.displayedRail.Hits()) == 0 {
		t.Fatal("test setup: the list frame should record the rail's hits")
	}

	m.prefs.fullLayout = true
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.fullFocus() {
		t.Fatalf("test setup: focus alpha full screen, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	preparedView(m)
	if len(m.displayedRail.Hits()) != 0 {
		t.Fatalf("full focus paints no rail, got %d hits", len(m.displayedRail.Hits()))
	}
}
