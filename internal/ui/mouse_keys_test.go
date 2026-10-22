package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestClickOnListFooterKeysOpensTheirModals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label string
		want  mode
	}{
		{"n new", modeForm},
		{"g group", modeGroupForm},
		{"? keys", modeHelp},
		{"s settings", modeSettings},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 320, 40
		createSession(t, m, "alpha", t.TempDir(), "")
		if m = clickLabel(t, m, tc.label); m.mode != tc.want {
			t.Errorf("click on %q: mode = %v, want %v (err %q)", tc.label, m.mode, tc.want, m.errBar.text)
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
	if m = leftClick(m, x, y); m.mode != modeList || !m.rail.Searching() || m.rail.Search() != "" {
		t.Fatalf("search owns the click, mode = %v searching = %v query = %q", m.mode, m.rail.Searching(), m.rail.Search())
	}
}

func TestClickOnConfirmFootAnswers(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.confirm.confirmTarget = confirmTarget{action: actionKill, label: "kill alpha? frees its RAM."}
	m.mode = modeConfirmDelete
	if m = clickLabel(t, m, "n/esc cancel"); m.mode != modeList || m.confirm.label != "" {
		t.Fatalf("cancel in the foot should close the confirm, mode = %v target = %+v", m.mode, m.confirm.confirmTarget)
	}
}

func TestClickOnHelpFootCloses(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	if m = clickLabel(t, m, "esc/q close"); m.mode != modeList {
		t.Fatalf("close in the foot should leave help, mode = %v", m.mode)
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
	if m = clickLabel(t, m, "esc"); m.mode != modeList {
		t.Fatalf("esc in the foot should close messages, mode = %v", m.mode)
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
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if m = clickLabel(t, m, "mouse back"); m.mode != modeList {
		t.Fatalf("the back pair in the foot should leave focus, mode = %v", m.mode)
	}
}

// A key runs when the button comes back up over it, so whatever it opens
// starts after the release. A press that slides off the key runs nothing.
func TestAKeyRunsOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "? keys")
	if m = leftPress(m, x, y); m.mode != modeList {
		t.Fatalf("the press alone ran the key, mode = %v", m.mode)
	}
	if m = leftRelease(m, 0, y); m.mode != modeList {
		t.Fatalf("a release off the key ran it, mode = %v", m.mode)
	}
	if m = leftRelease(m, x, y); m.mode != modeList {
		t.Fatalf("a second release with no press ran the key, mode = %v", m.mode)
	}
	if m = leftClick(m, x, y); m.mode != modeHelp {
		t.Fatalf("a click on the key should run it, mode = %v", m.mode)
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
	if m = leftRelease(m, x, y); m.mode != modeList {
		t.Fatalf("the release ran the repainted key, mode = %v", m.mode)
	}
}
