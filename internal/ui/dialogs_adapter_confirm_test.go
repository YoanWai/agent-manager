package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestConfirmKeyIgnoresUnboundKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"j", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}},
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}},
		{"space", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}},
		{"k", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}},
		{"Y", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")}},
		{"down", namedKey(tea.KeyDown)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			dir := t.TempDir()
			createSession(t, m, "alpha", dir, "")
			sess := m.sessionRows()[0]
			m.selectSessionRow(t, "alpha")
			if _, _ = m.killSelected(); !confirmOpen(m) {
				t.Fatalf("killSelected should open the confirm card, screen = %v", m.screenID())
			}
			before := pendingConfirm(m)

			_, cmd := m.Update(tc.key)
			if cmd != nil {
				t.Fatalf("an unbound key should issue no command, got %T", cmd())
			}
			if !confirmOpen(m) {
				t.Fatalf("screen = %v, want the card to stay up", m.screenID())
			}
			if pendingConfirm(m).action != before.action || len(pendingConfirm(m).sessions) != len(before.sessions) {
				t.Fatalf("confirm = %+v, want it untouched (%+v)", pendingConfirm(m), before)
			}
			if !m.services.tmux.Exists(sess.ID) {
				t.Fatal("an unbound key should not kill the session")
			}
		})
	}
}

func TestConfirmKeyCancels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"n", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}},
		{"esc", namedKey(tea.KeyEsc)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			dir := t.TempDir()
			createSession(t, m, "alpha", dir, "")
			sess := m.sessionRows()[0]
			m.selectSessionRow(t, "alpha")
			if _, _ = m.killSelected(); !confirmOpen(m) {
				t.Fatalf("killSelected should open the confirm card, screen = %v", m.screenID())
			}

			_, cmd := m.Update(tc.key)
			if cmd != nil {
				t.Fatalf("a cancel should issue no command, got %T", cmd())
			}
			if confirmOpen(m) {
				t.Fatalf("confirm open = %t, want the card gone", confirmOpen(m))
			}
			if !m.services.tmux.Exists(sess.ID) {
				t.Fatal("a cancel should leave the session alive")
			}
		})
	}
}

// ctrl+c over the list quits, card or no card: the drain starts and the
// session the card asked about lives.
func TestConfirmKeyCtrlCQuits(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	sess := m.sessionRows()[0]
	m.selectSessionRow(t, "alpha")
	if _, _ = m.killSelected(); !confirmOpen(m) {
		t.Fatalf("killSelected should open the confirm card, screen = %v", m.screenID())
	}

	m.Update(namedKey(tea.KeyCtrlC))
	if !m.effects.quitting {
		t.Fatal("ctrl+c over the confirm card did not quit")
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("ctrl+c should not kill the session")
	}
}

// A destructive answer is asked in a dialog, not on the status line, so a
// busy frame cannot hide the question.
func TestConfirmRendersAsADialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openConfirm(confirmTarget{
		action:   actionKill,
		sessions: []store.Session{{ID: "one", Name: "builder"}},
		label:    "kill builder? frees its RAM, v revives it.",
	})

	out := ansi.Strip(preparedView(m))
	for _, want := range []string{"Kill session", "kill builder?", "frees its RAM", "cancel"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirm dialog missing %q:\n%s", want, out)
		}
	}
	if status := ansi.Strip(m.statusLine()); strings.Contains(status, "builder") {
		t.Fatalf("the question moved to the dialog, status should not repeat it: %q", status)
	}
}

// Removing an SSH connection asks like any destructive act, and the yes
// queues the removal instead of a lifecycle request.
func TestConfirmRemovesAConnection(t *testing.T) {
	m := buildModel(t)
	m.confirmRemoveConnection("box")
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the confirm card", m.screenID())
	}
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "Remove connection") ||
		!strings.Contains(view, "remove connection box?") || !strings.Contains(view, "remove") {
		t.Fatalf("view = %q", view)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); cmd == nil {
		t.Fatal("confirm returned no effect command")
	}
	if !m.onList() || pendingConfirm(m).connection != "" {
		t.Fatalf("mode %v confirm %+v, want the dialog closed", m.screenID(), pendingConfirm(m))
	}
	var removed []string
	for _, lane := range m.lanes() {
		jobs := lane.pending
		if lane.active != nil {
			jobs = append([]*effectJob{lane.active}, jobs...)
		}
		for _, job := range jobs {
			switch request := job.request.(type) {
			case connectionRequest:
				if request.op == connectionRemove {
					removed = append(removed, request.name)
				}
			case lifecycleRequest:
				t.Fatalf("queued a lifecycle request %+v", request)
			}
		}
	}
	if len(removed) != 1 || removed[0] != "box" {
		t.Fatalf("removed %v, want box", removed)
	}
}
