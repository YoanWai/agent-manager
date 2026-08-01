package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Enter on a live session row focuses it; typed keys land in its pane and
// ctrl+q returns to the list without touching the pane.
func TestFocusModeForwardsKeys(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "focusme", t.TempDir(), "")
	m.selectSessionRow(t, "focusme")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	sess := m.rows[m.cursor].sess
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("ping-focus")},
		{Type: tea.KeyEnter},
	} {
		updated, _ := m.handleKey(msg)
		*m = *updated.(*Model)
	}
	if m.errBar.text != "" {
		t.Fatalf("forwarding set err: %q", m.errBar.text)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if strings.Contains(pane, "ping-focus") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("typed text never reached pane: %q", pane)
		}
		time.Sleep(30 * time.Millisecond)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("ctrl+q left mode = %v", m.mode)
	}
}

// A focused session that disappears drops the UI back to the list.
func TestFocusModeExitsWhenSessionDies(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	m.selectSessionRow(t, "doomed")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v", m.mode)
	}

	sess := m.rows[m.cursor].sess
	if err := m.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	m.tmux.Kill(sess.ID)
	m.applyCmd(t, m.refreshCmd())
	if m.mode != modeList {
		t.Fatalf("after session death, mode = %v", m.mode)
	}
}

// Leaving focus must hand the mouse back to the terminal, or native
// selection stays broken everywhere in the app after one focus session.
func TestFocusExitReleasesMouse(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "mouseback", t.TempDir(), "")
	m.selectSessionRow(t, "mouseback")

	updated, enterCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if enterCmd == nil {
		t.Fatal("entering focus issued no mouse command")
	}
	// Entering focus batches the mouse switch with the caret timer; the
	// message types are unexported, so compare against what the public
	// command produces.
	if !batchContains(enterCmd(), tea.EnableMouseCellMotion()) {
		t.Fatalf("entering focus never enabled mouse reporting: %T", enterCmd())
	}

	updated, exitCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if exitCmd == nil {
		t.Fatal("leaving focus issued no mouse command")
	}
	if m.sel.active {
		t.Fatal("selection survived leaving focus")
	}
}

// The rail marks which session is focused, so the mode is readable from
// the list as well as from the pane.
func TestRailShowsFocusBadge(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "badged", t.TempDir(), "")
	m.selectSessionRow(t, "badged")

	if strings.Contains(ansi.Strip(m.View()), "FOCUS") {
		t.Fatal("FOCUS badge shown before focusing")
	}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !strings.Contains(ansi.Strip(m.View()), "FOCUS") {
		t.Fatal("focused rail row carries no FOCUS badge")
	}
}

// batchContains reports whether a command's message is want, or a batch
// carrying a command that produces want.
func batchContains(msg tea.Msg, want tea.Msg) bool {
	if msg == want {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if batchContains(cmd(), want) {
			return true
		}
	}
	return false
}
