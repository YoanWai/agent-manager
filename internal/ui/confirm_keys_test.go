package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// handleConfirmKey drives the confirm dialog with the root as its host, the
// way the key dispatch does.
func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m, m.confirm.handleKey(m, msg)
}

// fakeConfirmHost records what the confirm dialog asks of the root.
type fakeConfirmHost struct {
	mode   mode
	queued []confirmTarget
	quits  int
}

func (h *fakeConfirmHost) confirmCard(title, question, consequence string, destructive bool, answer string) string {
	return title + "|" + question + "|" + consequence + "|" + answer
}
func (h *fakeConfirmHost) requestQuit() (tea.Model, tea.Cmd) { h.quits++; return nil, tea.Quit }
func (h *fakeConfirmHost) setMode(next mode)                 { h.mode = next }
func (h *fakeConfirmHost) queueLifecycle(target confirmTarget, allowLive bool, emptyNotice string) {
	h.queued = append(h.queued, target)
}
func (h *fakeConfirmHost) nextEffectCmd() tea.Cmd { return func() tea.Msg { return nil } }

func TestConfirmDialogWithFakeHost(t *testing.T) {
	sessions := []store.Session{{ID: "a"}}
	d := confirmDialog{confirmTarget{action: actionKill, sessions: sessions, label: "kill a? frees its RAM."}}
	h := &fakeConfirmHost{mode: modeConfirmDelete}

	if got := d.view(h); got != "✕ Kill session|kill a?|frees its RAM.|kill" {
		t.Fatalf("view = %q", got)
	}
	if cmd := d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); cmd != nil || h.mode != modeConfirmDelete {
		t.Fatalf("unbound key changed the dialog: mode %v", h.mode)
	}
	if cmd := d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); cmd == nil {
		t.Fatal("confirm returned no effect command")
	}
	if h.mode != modeList || len(h.queued) != 1 || h.queued[0].action != actionKill {
		t.Fatalf("mode %v queued %+v, want the captured kill queued and the dialog closed", h.mode, h.queued)
	}
	if &h.queued[0].sessions[0] == &sessions[0] {
		t.Fatal("queued target shares the dialog's session slice")
	}
	if d.action != "" || d.sessions != nil {
		t.Fatalf("dialog kept its target after confirming: %+v", d.confirmTarget)
	}
}

func TestConfirmKeyIgnoresUnboundKeys(t *testing.T) {
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
