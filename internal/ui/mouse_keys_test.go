package ui

import (
	"testing"

	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	tea "github.com/charmbracelet/bubbletea"
)

func TestClickOnListFooterGroupKeyOpensTheCard(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 320, 40
	createSession(t, m, "alpha", t.TempDir(), "")
	if m = clickLabel(t, m, "g group"); !m.groupFormOpen() {
		t.Fatalf("click on the group key did not open the card (err %q)", m.errBar.text)
	}
}

func TestClickOnListFooterSettingsKeyOpensTheCard(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 320, 40
	createSession(t, m, "alpha", t.TempDir(), "")
	if m = clickLabel(t, m, "s settings"); !m.settingsOpen() {
		t.Fatalf("click on the settings key did not open the card (err %q)", m.errBar.text)
	}
}

func TestClickOnListFooterKeysOpensTheirModals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label string
		open  func(*Model) bool
	}{
		{"n new", formOpen},
		{"g group", func(m *Model) bool { return m.groupFormOpen() }},
		{"? keys", func(m *Model) bool { _, ok := surfaceOf[*uihelp.Surface](m); return ok }},
		{"s settings", func(m *Model) bool { return m.settingsOpen() }},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 320, 40
		createSession(t, m, "alpha", t.TempDir(), "")
		if m = clickLabel(t, m, tc.label); !tc.open(m) {
			t.Errorf("click on %q opened nothing (err %q)", tc.label, m.errBar.text)
		}
	}
}

// A footer names keys the search field would take as typing, so the field
// keeps a click on them.
func TestClickOnListFooterWhileSearchingTypesNothing(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "n new")
	m.rail.SetSearch(m.rail.Search(), true)
	preparedView(m)
	if m = leftClick(m, x, y); !m.onList() || !m.rail.Searching() || m.rail.Search() != "" {
		t.Fatalf("search owns the click, screen = %v searching = %v query = %q", m.screenID(), m.rail.Searching(), m.rail.Search())
	}
}

func TestClickOnConfirmFootAnswers(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openConfirm(confirmTarget{action: actionKill, label: "kill alpha? frees its RAM."})
	if m = clickLabel(t, m, "n/esc cancel"); !m.onList() || pendingConfirm(m).label != "" {
		t.Fatalf("cancel in the foot should close the confirm, screen = %v target = %+v", m.screenID(), pendingConfirm(m))
	}
}

func TestClickOnHelpFootCloses(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	if m = clickLabel(t, m, "esc/q close"); !m.onList() {
		t.Fatalf("close in the foot should leave help, screen = %v", m.screenID())
	}
}

func TestClickOnMessagesFootRunsItsKeys(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openNotices("")
	if len(m.activeNotices()) == 0 {
		t.Fatal("test setup: a fresh model shows the welcome notice")
	}
	for _, key := range []string{"↑↓ pick", "pgup/pgdn"} {
		x, y := paintedCell(t, m, key)
		if _, ok := m.layout.keyHits.at(x, y); ok {
			t.Fatalf("%q moves the cursor; the rows and the wheel answer for it", key)
		}
	}
	if m = clickLabel(t, m, "esc"); !m.onList() {
		t.Fatalf("esc in the foot should close messages, screen = %v", m.screenID())
	}
}

func TestClickOnFocusFootLeavesFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if m = clickLabel(t, m, "mouse back"); !m.onList() {
		t.Fatalf("the back pair in the foot should leave focus, screen = %v", m.screenID())
	}
}

// A key runs when the button comes back up over it, so whatever it opens
// starts after the release. A press that slides off the key runs nothing.
func TestAKeyRunsOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "? keys")
	if m = leftPress(m, x, y); !m.onList() {
		t.Fatalf("the press alone ran the key, screen = %v", m.screenID())
	}
	if m = leftRelease(m, 0, y); !m.onList() {
		t.Fatalf("a release off the key ran it, screen = %v", m.screenID())
	}
	if m = leftRelease(m, x, y); !m.onList() {
		t.Fatalf("a second release with no press ran the key, screen = %v", m.screenID())
	}
	if m = leftClick(m, x, y); !helpOpen(m) {
		t.Fatalf("a click on the key should run it, screen = %v", m.screenID())
	}
}

// A key repainted under a held button is not the key the press chose, so
// the release runs nothing.
func TestARepaintedKeyDoesNotRunOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "? keys")
	m = leftPress(m, x, y)
	repainted := false
	for i, hit := range m.layout.keyHits {
		if hit.y == y && x >= hit.x0 && x < hit.x1 {
			m.layout.keyHits[i].key, repainted = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}, true
		}
	}
	if !repainted {
		t.Fatal("test setup: no key painted under the press")
	}
	if m = leftRelease(m, x, y); !m.onList() {
		t.Fatalf("the release ran the repainted key, screen = %v", m.screenID())
	}
}
