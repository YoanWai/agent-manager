package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func openSavedPromptsPanel(t *testing.T, m *Model) {
	t.Helper()
	m.runBatch(t, m.openSettings())
	m.settings.dialog.Field = uisettings.FieldPrompts
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter}))
	if !m.settings.dialog.Prompts.Open() {
		t.Fatalf("enter on the saved prompts row did not open them: %q", m.errBar.text)
	}
}

func pressSettings(m *Model, msg tea.KeyMsg) tea.Cmd {
	_, cmd := m.handleSettingsKey(msg)
	return cmd
}

func pasteSettings(m *Model, text string) tea.Cmd {
	return pressSettings(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
}

func savePrompt(t *testing.T, m *Model, name, text string) {
	t.Helper()
	if err := m.services.store.SaveSessionPreset("", store.SessionPreset{Name: name, Instructions: text}); err != nil {
		t.Fatal(err)
	}
}

func storedPrompts(t *testing.T, m *Model) []store.SessionPreset {
	t.Helper()
	rows, err := m.services.store.SessionPresets()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// Settings lists saved prompts as a row of its own.
func TestSettingsShowsTheSavedPromptsRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.runBatch(t, m.openSettings())
	if !strings.Contains(ansi.Strip(m.viewSettings()), "saved prompts") {
		t.Fatal("Settings has no saved prompts row")
	}
}

// A save, an edit, a discarded edit and a delete each reach the store on
// the effect lane, never inside Update.
func TestSavedPromptsPanelWritesThroughTheEffectLane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	openSavedPromptsPanel(t, m)
	pressSettings(m, runeKey("n"))
	pasteSettings(m, "  Role  ")
	pressSettings(m, tea.KeyMsg{Type: tea.KeyTab})
	pasteSettings(m, " \n literal\n\n")
	save := pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if save == nil || len(storedPrompts(t, m)) != 0 {
		t.Fatal("ctrl+s wrote inside Update")
	}
	if again := pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}); again != nil {
		t.Fatal("a second ctrl+s queued a second save")
	}
	m.runBatch(t, save)
	rows := storedPrompts(t, m)
	if len(rows) != 1 || rows[0].Name != "Role" || rows[0].Instructions != " \n literal\n\n" {
		t.Fatalf("saved %+v", rows)
	}

	pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	pasteSettings(m, "Renamed")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
	if rows = storedPrompts(t, m); len(rows) != 1 || rows[0].Name != "Renamed" || rows[0].Instructions != " \n literal\n\n" {
		t.Fatalf("rename saved %+v", rows)
	}

	pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressSettings(m, tea.KeyMsg{Type: tea.KeyTab})
	pasteSettings(m, "discard")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEsc}))
	if rows = storedPrompts(t, m); strings.Contains(rows[0].Instructions, "discard") {
		t.Fatal("esc saved the edit")
	}

	pressSettings(m, runeKey("d"))
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter}))
	if rows = storedPrompts(t, m); len(rows) != 0 {
		t.Fatalf("delete left %+v", rows)
	}
	if !m.settings.dialog.Prompts.Open() || m.mode != modeSettings {
		t.Fatal("the panel closed under its own delete")
	}
}

// A name the catalog already holds is refused with the typing kept.
func TestSavedPromptsPanelKeepsTypingOnARefusedSave(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Taken", "existing")
	openSavedPromptsPanel(t, m)
	pressSettings(m, runeKey("n"))
	pasteSettings(m, "Taken")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
	if m.errBar.text == "" || !strings.Contains(ansi.Strip(m.viewSettings()), "Taken") {
		t.Fatalf("refused save: err %q", m.errBar.text)
	}
	pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	pasteSettings(m, "Fresh")
	pressSettings(m, tea.KeyMsg{Type: tea.KeyTab})
	pasteSettings(m, "text")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
	if rows := storedPrompts(t, m); len(rows) != 2 || m.errBar.text != "" {
		t.Fatalf("retry saved %+v, err %q", rows, m.errBar.text)
	}
}

// A read that answers after the panel was closed and opened again leaves
// the newer list alone.
func TestSavedPromptsPanelIgnoresALateRead(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.runBatch(t, m.openSettings())
	m.settings.dialog.Field = uisettings.FieldPrompts
	late := pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter})()
	pressSettings(m, tea.KeyMsg{Type: tea.KeyEsc})
	savePrompt(t, m, "New", "text")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter}))
	m.Update(late)
	if body := ansi.Strip(m.viewSettings()); !strings.Contains(body, "New") || strings.Contains(body, "No saved prompts") {
		t.Fatalf("a late read replaced the list:\n%s", body)
	}
}

// A click on the row opens the panel, the wheel walks a long list, and
// everything stays inside a small terminal.
func TestSavedPromptsPanelByMouseInASmallTerminal(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 40, 22
	for i := range 30 {
		savePrompt(t, m, fmt.Sprintf("Role %02d", i), "text")
	}
	m.runBatch(t, m.openSettings())
	for range uisettings.FieldPrompts {
		m.handleMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	}
	frame := m.viewSettings()
	if m.settings.dialog.Field != uisettings.FieldPrompts || len(strings.Split(frame, "\n")) > m.layout.height {
		t.Fatalf("the wheel stopped on field %d", m.settings.dialog.Field)
	}
	clicked := false
	for y, line := range strings.Split(ansi.Strip(frame), "\n") {
		if strings.Contains(line, "saved prompts") {
			_, cmd := m.handleSettingsClick(m.layout.cardLeft+4, y)
			m.runBatch(t, cmd)
			clicked = true
			break
		}
	}
	if !clicked || !m.settings.dialog.Prompts.Open() {
		t.Fatal("a click on the saved prompts row did not open them")
	}
	for range 29 {
		m.handleMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	}
	frame = m.viewSettings()
	if !strings.Contains(frame, "Role 29") {
		t.Fatal("the wheel did not bring the last prompt into view")
	}
	lines := strings.Split(frame, "\n")
	if len(lines) > m.layout.height {
		t.Fatalf("frame has %d rows for a %d row terminal", len(lines), m.layout.height)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.layout.width {
			t.Fatalf("frame line wider than the terminal: %q", line)
		}
	}
	pressSettings(m, runeKey("e"))
	if frame = m.viewSettings(); len(strings.Split(frame, "\n")) > m.layout.height || !strings.Contains(frame, "ctrl+s") {
		t.Fatal("the editor does not fit the terminal")
	}
}

// A click on the blank line under the title opens nothing while the saved
// prompts row is scrolled out of view above the docs row.
func TestSettingsClickAboveTheBodyLeavesSavedPromptsClosed(t *testing.T) {
	m := buildModel(t)
	openBrowser = func(url string) error { t.Fatalf("opened %s", url); return nil }
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })
	m.runBatch(t, m.openSettings())
	d := &m.settings.dialog
	d.Field = uisettings.FieldCount - 1
	for m.layout.width, m.layout.height = 40, 30; m.layout.height > 8; m.layout.height-- {
		if m.viewSettings(); d.PromptsRow == -1 && d.DocsRow >= 0 {
			break
		}
	}
	if d.PromptsRow != -1 || d.DocsRow < 0 {
		t.Fatalf("no height hides saved prompts and shows docs: prompts %d docs %d", d.PromptsRow, d.DocsRow)
	}
	_, cmd := m.handleSettingsClick(m.layout.cardLeft+4, m.layout.cardTop+1)
	m.runBatch(t, cmd)
	if d.Prompts.Open() {
		t.Fatal("a click above the body opened saved prompts")
	}
}
