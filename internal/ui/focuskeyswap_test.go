package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The caret blinks while focused and stops when focus ends.
func TestCursorBlinks(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "blinker", t.TempDir(), "")
	m.selectSessionRow(t, "blinker")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !m.cursorOn {
		t.Fatal("caret starts hidden")
	}

	updated, cmd := m.Update(cursorBlinkMsg{})
	*m = *updated.(*Model)
	if m.cursorOn {
		t.Fatal("caret did not blink off")
	}
	if cmd == nil {
		t.Fatal("blink timer was not re-armed while focused")
	}

	// Typing must show the caret again immediately.
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	*m = *updated.(*Model)
	if !m.cursorOn {
		t.Fatal("typing left the caret hidden")
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	*m = *updated.(*Model)
	if _, cmd := m.Update(cursorBlinkMsg{}); cmd != nil {
		t.Fatal("blink timer kept running after focus ended")
	}
}

// The setting swaps which key focuses and which attaches, and persists.
func TestSettingsSwapsFocusKey(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settings.enterFocuses {
		t.Fatal("settings should open with enter focusing")
	}
	card := ansi.Strip(m.viewSettings())
	if !strings.Contains(card, "session keys") {
		t.Fatalf("settings card has no session keys row:\n%s", card)
	}
	for i := 0; i < 4; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldFocusKey {
		t.Fatalf("fourth down should focus the session keys field, got %d", m.settings.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if !strings.Contains(ansi.Strip(m.viewSettings()), "attach") {
		t.Fatal("swapped card does not read attach on enter")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.enterFocuses() {
		t.Fatal("swapped choice did not persist")
	}
}

// With the keys swapped, Enter attaches and A focuses.
func TestSwappedKeysRouteActions(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "swapped", t.TempDir(), "")
	m.selectSessionRow(t, "swapped")

	// Default: enter focuses.
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("enter did not focus by default, mode = %v", m.mode)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	*m = *updated.(*Model)

	// Swap through the settings screen, the same path a user takes.
	m.openSettings()
	m.settings.field = settingsFieldFocusKey
	m.cycleSetting(1)
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(focusKeySetting); err != nil || chosen != "attach" {
		t.Fatalf("swap did not persist, chosen = %q, err = %v", chosen, err)
	}
	// Swapped: A focuses instead.
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("A did not focus after the swap, mode = %v, err = %q", m.mode, m.err)
	}
}
