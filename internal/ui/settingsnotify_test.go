package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNotifyCommandRowLabel(t *testing.T) {
	long := `curl -H "Title: $AM_TITLE" -d "$AM_BODY" https://ntfy.example.com/agents`
	for _, tc := range []struct{ value, want string }{
		{"", "off"},
		{"say done", "say done"},
		{long, string([]rune(long)[:notifyCommandLabelRunes-1]) + "…"},
	} {
		if got := (notifyCommandRow{value: tc.value}).label(); got != tc.want {
			t.Errorf("label(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func notifyCommandSettings(t *testing.T, m *Model) {
	t.Helper()
	m.applyCmd(t, m.openSettings())
	m.settings.field = settingsFieldNotifyCommand
}

// The command is typed: while the field is open every key is text, enter
// keeps it, and leaving Settings stores it where the poller reads it.
func TestSettingsNotifyCommandIsTypedAndStored(t *testing.T) {
	m := buildModel(t)
	notifyCommandSettings(t, m)
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "notify command") || !strings.Contains(view, "off") {
		t.Fatalf("the row should read off before anything is set:\n%s", view)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settings.notifyCommand.typing || m.mode != modeSettings {
		t.Fatal("enter should open the field, not save")
	}
	m.pressInSettings(t, runeKey(`curl -d "$AM_BODY" ntfy.sh/jk`))
	if m.settings.field != settingsFieldNotifyCommand {
		t.Fatal("a typed j or k must not move the cursor off the row")
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeList {
		t.Fatalf("esc should save and leave, mode = %v", m.mode)
	}
	if got, err := storedNotifyCommand(m.store); err != nil || got != `curl -d "$AM_BODY" ntfy.sh/jk` {
		t.Fatalf("stored command = %q, %v", got, err)
	}

	notifyCommandSettings(t, m)
	if got := m.settings.notifyCommand.label(); got != `curl -d "$AM_BODY" ntfy.sh/jk` {
		t.Fatalf("reopened Settings shows %q", got)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	for range len(`curl -d "$AM_BODY" ntfy.sh/jk`) {
		m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if got, err := storedNotifyCommand(m.store); err != nil || got != "" {
		t.Fatalf("an emptied field should turn the command off, stored %q, %v", got, err)
	}
}

func TestSettingsNotifyCommandEscDropsWhatWasTyped(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(notifyCommandSetting, "say done"); err != nil {
		t.Fatal(err)
	}
	notifyCommandSettings(t, m)
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, runeKey(" junk"))
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settings.notifyCommand.typing || m.mode != modeSettings || m.settings.notifyCommand.value != "say done" {
		t.Fatalf("esc should close the field and keep the old command, got %q", m.settings.notifyCommand.value)
	}
}

// A command Settings could not read is not saved back as the empty line
// the row fell back to, unless the user typed one.
func TestSettingsKeepsANotifyCommandItCouldNotRead(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(notifyCommandSetting, "say done"); err != nil {
		t.Fatal(err)
	}
	notifyCommandSettings(t, m)
	m.settings.notifyCommand = notifyCommandRow{}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if got, err := storedNotifyCommand(m.store); err != nil || got != "say done" {
		t.Fatalf("stored command = %q, %v; want it kept", got, err)
	}

	notifyCommandSettings(t, m)
	m.settings.notifyCommand = notifyCommandRow{}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, runeKey("say typed"))
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if got, err := storedNotifyCommand(m.store); err != nil || got != "say typed" {
		t.Fatalf("stored command = %q, %v; want what was typed", got, err)
	}
}
