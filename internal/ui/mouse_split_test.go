package ui

import (
	"testing"
)

func focusedModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	return m
}

// A press anywhere in the rail column leaves focus on the press, the blank
// below the last row included, not only on a row.
func TestPressOnTheRailsBlankLeavesFocus(t *testing.T) {
	t.Parallel()
	m := focusedModel(t)
	preparedView(m)
	y0, _ := m.bodyYRange()
	blank := -1
	for line, row := range railHitRows(m) {
		if row < 0 && line > 0 {
			blank = y0 + line
			break
		}
	}
	if blank < 0 {
		t.Fatal("test setup: the rail paints no line without a row")
	}
	if m = leftPress(m, 2, blank); m.mode != modeList {
		t.Fatalf("a press on the rail's blank should leave focus, mode = %v", m.mode)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("leaving should keep the cursor on alpha, got %q", sess.Name)
	}
}

func TestPressOnTheDividerKeepsFocus(t *testing.T) {
	t.Parallel()
	m := focusedModel(t)
	preparedView(m)
	y0, _ := m.bodyYRange()
	for _, x := range []int{m.dividerX(), m.dividerX() + splitHitSlop} {
		if m = leftPress(m, x, y0+1); m.mode != modeFocus {
			t.Fatalf("a press on the divider at %d left focus", x)
		}
	}
}

// The preview only focuses a session that focus would open: a group, an
// archived row, the search field and the quick bar keep the press, and
// full screen has no preview beside the list.
func TestPreviewPressFocusesOnlyALiveSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"group", func(m *Model) { m.selectGroupRow(t, "work") }},
		{"search", func(m *Model) { m.rail.SetSearch(m.rail.Search(), true) }},
		{"quick bar", func(m *Model) { m.openQuickMode() }},
		{"full screen", func(m *Model) { m.prefs.fullLayout = true }},
	} {
		m := buildModel(t)
		if err := m.services.store.CreateGroup("work", ""); err != nil {
			t.Fatal(err)
		}
		m.applyCmd(t, m.refreshCmd())
		createSession(t, m, "alpha", t.TempDir(), "work")
		m.selectSessionRow(t, "alpha")
		tc.setup(m)
		preparedView(m)
		y0, _ := m.bodyYRange()
		if m = leftPress(m, m.layout.width-3, y0+1); m.mode == modeFocus {
			t.Errorf("%s: a press on the preview focused", tc.name)
		}
	}
}
