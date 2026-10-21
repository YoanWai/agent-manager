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
			if _, _ = m.killSelected(); m.mode != modeConfirmDelete {
				t.Fatalf("killSelected should open the confirm card, mode = %v", m.mode)
			}
			before := m.confirm

			_, cmd := m.handleConfirmKey(tc.key)
			if cmd != nil {
				t.Fatalf("an unbound key should issue no command, got %T", cmd())
			}
			if m.mode != modeConfirmDelete {
				t.Fatalf("mode = %v, want the card to stay up", m.mode)
			}
			if m.confirm.action != before.action || len(m.confirm.sessions) != len(before.sessions) {
				t.Fatalf("confirm = %+v, want it untouched (%+v)", m.confirm, before)
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
			if _, _ = m.killSelected(); m.mode != modeConfirmDelete {
				t.Fatalf("killSelected should open the confirm card, mode = %v", m.mode)
			}

			_, cmd := m.handleConfirmKey(tc.key)
			if cmd != nil {
				t.Fatalf("a cancel should issue no command, got %T", cmd())
			}
			if m.mode != modeList {
				t.Fatalf("mode = %v, want modeList after a cancel", m.mode)
			}
			if m.confirm.action != "" || m.confirm.sessions != nil {
				t.Fatalf("confirm = %+v, want the zero confirmTarget", m.confirm)
			}
			if !m.services.tmux.Exists(sess.ID) {
				t.Fatal("a cancel should leave the session alive")
			}
		})
	}
}

func TestConfirmKeyCtrlCQuits(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	sess := m.sessionRows()[0]
	m.selectSessionRow(t, "alpha")
	if _, _ = m.killSelected(); m.mode != modeConfirmDelete {
		t.Fatalf("killSelected should open the confirm card, mode = %v", m.mode)
	}
	before := m.confirm

	_, cmd := m.handleConfirmKey(namedKey(tea.KeyCtrlC))
	if cmd == nil {
		t.Fatal("ctrl+c should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c should produce tea.QuitMsg, got %T", cmd())
	}
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the card left alone", m.mode)
	}
	if m.confirm.action != before.action {
		t.Fatalf("confirm = %+v, want it untouched (%+v)", m.confirm, before)
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
	m.mode = modeConfirmDelete
	m.confirm.confirmTarget = confirmTarget{
		action:   actionKill,
		sessions: []store.Session{{ID: "one", Name: "builder"}},
		label:    "kill builder? frees its RAM, v revives it.",
	}

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
