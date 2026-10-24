package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

func confirmOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.ConfirmSurface](m)
	return ok
}

// pendingConfirm is the request the open card holds, zero when none is open.
func pendingConfirm(m *Model) confirmTarget {
	if s, ok := surfaceOf[*uidialogs.ConfirmSurface](m); ok {
		return s.Root().(confirmRoot).target
	}
	return confirmTarget{}
}

func openKillConfirm(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.openConfirm(confirmTarget{action: actionKill, label: "kill alpha? frees its RAM."})
	return m
}

func TestConfirmClosesOnEscNAndBackdrop(t *testing.T) {
	for name, close := range map[string]func(*Model) *Model{
		"esc":      func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return m },
		"n":        func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); return m },
		"backdrop": func(m *Model) *Model { return clickBackdrop(t, m) },
	} {
		m := openKillConfirm(t)
		if !confirmOpen(m) || !mustTop(t, m).Dismissible() {
			t.Fatalf("%s: setup: confirm open=%t", name, confirmOpen(m))
		}
		m = close(m)
		if confirmOpen(m) || m.effects.Quitting || !m.onList() {
			t.Errorf("%s: open=%t quitting=%t screen = %v, want the card closed and the manager running", name, confirmOpen(m), m.effects.Quitting, m.screenID())
		}
	}
}

func TestConfirmAcceptPopsOnceAndRunsTheRequest(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess := railSelectedSession(m)
	m.openConfirm(confirmTarget{action: actionKill, sessions: []store.Session{sess}, label: "kill alpha?"})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if confirmOpen(m) || len(m.surfaces) != 0 {
		t.Fatalf("surfaces = %d, want the card gone", len(m.surfaces))
	}
	if m.effects.Main.Active == nil && len(m.effects.Main.Pending) == 0 {
		t.Fatal("accepting queued no lifecycle request")
	}
}

func mustTop(t *testing.T, m *Model) uisurface.Surface {
	t.Helper()
	top, ok := m.top()
	if !ok {
		t.Fatal("no surface on top")
	}
	return top
}
