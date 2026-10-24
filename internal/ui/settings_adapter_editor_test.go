package ui

import (
	"strings"
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// editorSettings opens Settings on the editor row with the probe already
// answered for a machine that has these editors.
func editorSettings(t *testing.T, m *Model, installed ...string) {
	t.Helper()
	captureEditor(t, installed...)
	m.applyCmd(t, m.openSettings())
	m.settingsCard().State().Field = uisettings.FieldEditor
}

func (m *Model) pressInSettings(t *testing.T, msg tea.KeyMsg) {
	t.Helper()
	updated, _ := m.handleKey(msg)
	*m = *updated.(*Model)
}

// The editors the shell names are offered beside the ones found on PATH,
// once each, so a terminal editor is a step away rather than a typed line.
func TestProbeOffersTheShellsEditors(t *testing.T) {
	captureEditor(t, "code")
	t.Setenv("VISUAL", "code")
	t.Setenv("EDITOR", " nvim ")
	probed := detectedEditors().probe()
	if got := strings.Join(probed.Found, ","); got != "code,nvim" {
		t.Fatalf("found = %q, want code once and nvim", got)
	}
	row := uisettings.NewEditorRow("nvim", &probed)
	if row.Custom() || row.Label() != "nvim" {
		t.Fatalf("a stored nvim should sit on its own entry, got %q custom %v", row.Label(), row.Custom())
	}
}

// The picked editor is stored, survives a restart, and is what the editor
// key opens, ahead of the one auto would have taken.
func TestSettingsEditorRowStoresThePickedEditor(t *testing.T) {
	m := buildModel(t)
	editorSettings(t, m, "code", "zed")
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "auto · code") {
		t.Fatalf("the row should name what auto opens:\n%s", view)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyRight})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyRight})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsOpen() {
		t.Fatalf("esc should save and leave, screen = %v", m.screenID())
	}
	m.drainEffects(t)
	if stored, err := m.services.store.Editor(); err != nil || stored != "zed" {
		t.Fatalf("stored editor = %q, %v", stored, err)
	}
	if got := reloadModel(t, m).captureEditorResolution().editorLine(); got != "zed" {
		t.Fatalf("a restarted manager opens %q, want zed", got)
	}
}

// Custom is the one entry that is typed. While the field is open every key
// is text, so a letter that moves the cursor elsewhere types instead.
func TestSettingsEditorCustomLineIsTyped(t *testing.T) {
	m := buildModel(t)
	editorSettings(t, m, "code")
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyLeft})
	if !m.settingsCard().State().Editor.Custom() {
		t.Fatalf("left from auto should land on custom, got %q", m.settingsCard().State().Editor.Label())
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().Editor.Typing() || !m.settingsOpen() {
		t.Fatal("enter on custom should open the field, not save")
	}
	m.pressInSettings(t, runeKey("junk"))
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsCard().State().Editor.Typing() || m.settingsCard().State().Editor.Line() != "" || !m.settingsOpen() {
		t.Fatalf("esc should drop what was typed and stay in Settings, line = %q", m.settingsCard().State().Editor.Line())
	}

	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, runeKey("code -n k"))
	if m.settingsCard().State().Field != uisettings.FieldEditor {
		t.Fatal("a typed k must not move the cursor off the row")
	}
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "code -n k") {
		t.Fatalf("the field should show what is typed:\n%s", view)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyBackspace})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyBackspace})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.settingsCard().State().Editor.Label(); got != "custom · code -n" {
		t.Fatalf("label = %q", got)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	if stored, err := m.services.store.Editor(); err != nil || stored != "code -n" {
		t.Fatalf("stored editor = %q, %v", stored, err)
	}
	if got := m.captureEditorResolution().editorLine(); got != "code -n" {
		t.Fatalf("resolved editor = %q", got)
	}
}

// Stepping back to auto clears the stored line, so the environment and
// PATH decide again.
func TestSettingsEditorRowReturnsToAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetEditor("code -n"); err != nil {
		t.Fatalf("SetEditor: %v", err)
	}
	m.services.editor = "code -n"
	editorSettings(t, m, "zed")
	if got := m.settingsCard().State().Editor.Label(); got != "custom · code -n" {
		t.Fatalf("label = %q", got)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyRight})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	if stored, err := m.services.store.Editor(); err != nil || stored != "" {
		t.Fatalf("stored editor = %q, %v", stored, err)
	}
	if got := m.captureEditorResolution().editorLine(); got != "zed" {
		t.Fatalf("resolved editor = %q, want the editor on PATH", got)
	}
}
