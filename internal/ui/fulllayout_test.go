package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionLayoutDefaultsToSplit(t *testing.T) {
	m := buildModel(t)
	if m.fullLayout {
		t.Fatal("split should be the default sessions layout")
	}
	if err := m.store.SetSetting(sessionLayoutSetting, "full"); err != nil {
		t.Fatal(err)
	}
	if !storedFullLayout(m.store) {
		t.Fatal("stored full choice should turn the full layout on")
	}
}

func TestZTogglesSessionLayout(t *testing.T) {
	m := buildModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if !m.fullLayout {
		t.Fatal("z should turn the full layout on")
	}
	if chosen, err := m.store.Setting(sessionLayoutSetting); err != nil || chosen != "full" {
		t.Fatalf("want stored full, got %q err %v", chosen, err)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if m.fullLayout {
		t.Fatal("a second z should return to the split layout")
	}
	if chosen, err := m.store.Setting(sessionLayoutSetting); err != nil || chosen != "split" {
		t.Fatalf("want stored split, got %q err %v", chosen, err)
	}
}

func TestSettingsTogglesSessionLayout(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.fullLayout {
		t.Fatal("settings should open on split by default")
	}
	for m.settings.field != settingsFieldSessionLayout {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(sessionLayoutSetting); err != nil || chosen != "full" {
		t.Fatalf("want stored full, got %q err %v", chosen, err)
	}
	if !m.fullLayout {
		t.Fatal("the model should mirror the saved full choice")
	}
}

// The full screen frame is the rail alone: no preview column, so the
// captured pane and the detail head stay with the split layout.
func TestFullLayoutFrameHasNoPreviewColumn(t *testing.T) {
	m := shotModel()
	split := ansi.Strip(m.View())
	if !strings.Contains(split, "token bucket limiter") {
		t.Fatalf("split frame lost its preview:\n%s", split)
	}
	m.fullLayout = true
	full := ansi.Strip(m.View())
	if strings.Contains(full, "token bucket limiter") {
		t.Fatalf("full screen frame still paints the preview:\n%s", full)
	}
	if !strings.Contains(full, "add-rate-limiting") {
		t.Fatalf("full screen frame lost the session tree:\n%s", full)
	}
	rows := strings.Split(m.View(), "\n")
	if len(rows) != m.height {
		t.Fatalf("full screen frame is %d rows, terminal is %d", len(rows), m.height)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got > m.width {
			t.Fatalf("full screen frame row is %d wide, terminal is %d", got, m.width)
		}
	}
}

// Meters and messages leave the rail foot in full screen for one condensed
// line: the machine readings inline, and the messages count when any exist.
func TestFullLayoutFootLine(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	foot := ansi.Strip(strings.Join(m.railFootLines(m.width-1), "\n"))
	for _, want := range []string{"cpu 22%", "mem 75%", "net"} {
		if !strings.Contains(foot, want) {
			t.Errorf("condensed foot line misses %q:\n%s", want, foot)
		}
	}
	if strings.Contains(foot, "messages") {
		t.Fatalf("no notices should mean no messages count:\n%s", foot)
	}
	if lines := m.railFootLines(m.width - 1); len(lines) != 1 {
		t.Fatalf("full screen foot should be one line, got %d", len(lines))
	}
}

func TestFullLayoutFootLineCountsMessages(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 120, 34
	m.fullLayout = true
	foot := ansi.Strip(strings.Join(m.railFootLines(m.width-1), "\n"))
	if !strings.Contains(foot, "messages") {
		t.Fatalf("active notices should show a messages count:\n%s", foot)
	}
	full := ansi.Strip(m.View())
	if !strings.Contains(full, "messages") {
		t.Fatalf("full screen frame lost the messages count:\n%s", full)
	}
}

// The full screen footer keeps the split's tiers and adds the key that
// returns to the split.
func TestFullLayoutFooterNamesZ(t *testing.T) {
	m := shotModel()
	split := ansi.Strip(m.viewFooter())
	if strings.Contains(split, "split view") {
		t.Fatalf("split footer should not offer the split it is in:\n%s", split)
	}
	m.fullLayout = true
	full := ansi.Strip(m.viewFooter())
	if !strings.Contains(full, "z split view") {
		t.Fatalf("full screen footer misses z:\n%s", full)
	}
}
