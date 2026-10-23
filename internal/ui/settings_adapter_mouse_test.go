package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

func settingsModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 50
	m.applyCmd(t, m.openSettings())
	return m
}

func TestClickOnASettingsRowMovesThenSteps(t *testing.T) {
	m := settingsModel(t)
	x, y := paintedCell(t, m, "background")
	m = leftClick(m, x, y)
	d := m.settingsCard().State()
	if d.Field != uisettings.FieldBackground || d.TerminalBackground {
		t.Fatalf("first click should only focus the row, field %d background %v", d.Field, d.TerminalBackground)
	}
	m = leftClick(m, x, y)
	if !m.settingsCard().State().TerminalBackground || !m.prefs.terminalBackground {
		t.Fatal("second click should step the row and preview it")
	}
	m = wheel(m, tea.MouseButtonWheelDown, x, y)
	if m.settingsCard().State().Field != uisettings.FieldBackground+1 {
		t.Fatalf("the wheel should move to the next field, field %d", m.settingsCard().State().Field)
	}
}

func TestClickOnTheCLIsRowManagesThem(t *testing.T) {
	m := settingsModel(t)
	x, y := paintedCell(t, m, "CLIs")
	m = leftClick(m, x, y)
	m = leftClick(m, x, y)
	if !m.settingsCard().State().CLIPicker {
		t.Fatal("two clicks on CLIs should open the picker")
	}
	names := m.settingsCard().State().CLINames
	if len(names) < 2 {
		t.Fatalf("test setup: the picker needs two CLIs, has %v", names)
	}
	x, y = paintedCell(t, m, "] "+names[1])
	if m = leftClick(m, x, y); !m.settingsCard().State().CLIHidden[names[1]] {
		t.Fatalf("one click on %s's checkbox should toggle it", names[1])
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.settingsCard().State().CLICursor != 2 {
		t.Fatalf("the wheel should move the picker cursor, at %d", m.settingsCard().State().CLICursor)
	}
}

func TestClickOnTheKeyPickerSetsAKey(t *testing.T) {
	m := settingsModel(t)
	x, y := paintedCell(t, m, "keybindings")
	m = leftClick(m, x, y)
	m = leftClick(m, x, y)
	if !m.settingsCard().State().KeyPicker {
		t.Fatal("two clicks on keybindings should open the picker")
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.settingsCard().State().KeyCursor != 1 {
		t.Fatalf("the wheel should move the picker cursor, at %d", m.settingsCard().State().KeyCursor)
	}
	x, y = paintedCell(t, m, "editor  ")
	m = leftClick(m, x, y)
	if row := m.settingsCard().State().PickedRow(); row.Action.Name != "editor" || m.settingsCard().State().KeyCapture {
		t.Fatalf("first click should move to editor, at %q capture %v", row.Action.Name, m.settingsCard().State().KeyCapture)
	}
	if m = leftClick(m, x, y); !m.settingsCard().State().KeyCapture {
		t.Fatal("a click on the row under the cursor should start capture")
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.settingsCard().State().PickedRow().Action.Name != "editor" {
		t.Fatal("the wheel must not move the row a capture is binding")
	}
}
