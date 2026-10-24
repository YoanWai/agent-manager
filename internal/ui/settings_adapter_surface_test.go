package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
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
	if !m.settingsOpen() || !m.onList() {
		t.Fatalf("settings open %v, mode %v", m.settingsOpen(), m.screenID())
	}
}

// esc and the save button close the card the same way, and the close
// queues the save once.
func TestSettingsEscClosesAndSavesOnce(t *testing.T) {
	t.Parallel()
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}} {
		m := openedSettings(t)
		m.settingsCard().State().Field = uisettings.FieldMouse
		m.cycleSetting(1)
		before := m.settings.Pending
		_, cmd := m.Update(k)
		if m.settingsOpen() || len(m.surfaces) != 0 {
			t.Fatalf("%v left %d surfaces", k, len(m.surfaces))
		}
		if m.settings.Pending != before+1 {
			t.Fatalf("%v queued %d saves, want one", k, m.settings.Pending-before)
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
	stale := uipersist.LoadedMsg{Request: uipersist.LoadRequest{Target: uipersist.DialogTarget, Generation: m.settings.Gen - 1}}
	m.settingsCard().State().Field = uisettings.FieldMouse
	m.handleSettingsLoaded(stale)
	if m.settingsCard().State().Field != uisettings.FieldMouse {
		t.Fatal("an earlier visit's load rebuilt the card")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.handleSettingsLoaded(uipersist.LoadedMsg{Request: uipersist.LoadRequest{Target: uipersist.DialogTarget, Generation: m.settings.Gen}})
	if m.settingsOpen() {
		t.Fatal("a load reopened a closed card")
	}
}
