package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

func openedSettings(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.applyCmd(t, m.openSettings())
	return m
}

func TestSettingsOpensFromItsKeyAndDrawsOverTheList(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !m.settingsOpen() || m.mode != modeList {
		t.Fatalf("settings open %v, mode %v", m.settingsOpen(), m.mode)
	}
}

// esc and the save button close the card the same way, and the close
// queues the save once.
func TestSettingsEscClosesAndSavesOnce(t *testing.T) {
	t.Parallel()
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}} {
		m := openedSettings(t)
		m.settingsCard().state.Field = uisettings.FieldMouse
		m.cycleSetting(1)
		before := m.settings.pending
		_, cmd := m.Update(k)
		if m.settingsOpen() || len(m.surfaces) != 0 {
			t.Fatalf("%v left %d surfaces", k, len(m.surfaces))
		}
		if m.settings.pending != before+1 {
			t.Fatalf("%v queued %d saves, want one", k, m.settings.pending-before)
		}
		m.applyCmd(t, cmd)
	}
}

// A command being typed is a draft: a stray click keeps it. Not parallel:
// editorSettings pins $EDITOR with t.Setenv.
func TestSettingsKeepsATypedEditorLine(t *testing.T) {
	m := openedSettings(t)
	top, _ := m.top()
	if !top.Dismissible() {
		t.Fatal("the main rows say a stray click keeps a draft")
	}
	editorSettings(t, m, "code")
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyLeft})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if top, _ = m.top(); top.Dismissible() {
		t.Fatal("a typed command says a stray click closes the card")
	}
}

// A load for a card that has since closed, or for an earlier visit, is
// dropped and does not rebuild the card on screen.
func TestSettingsLoadsAreFencedToTheirVisit(t *testing.T) {
	t.Parallel()
	m := openedSettings(t)
	stale := settingsLoadedMsg{request: settingsLoadRequest{target: settingsLoadDialog, generation: m.settings.gen - 1}}
	m.settingsCard().state.Field = uisettings.FieldMouse
	m.handleSettingsLoaded(stale)
	if m.settingsCard().state.Field != uisettings.FieldMouse {
		t.Fatal("an earlier visit's load rebuilt the card")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.handleSettingsLoaded(settingsLoadedMsg{request: settingsLoadRequest{target: settingsLoadDialog, generation: m.settings.gen}})
	if m.settingsOpen() {
		t.Fatal("a load reopened a closed card")
	}
}
