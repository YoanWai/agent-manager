package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsCard is the open Settings card; a test that reads it with none up
// is wrong, so it panics.
func (m *Model) settingsCard() *settingsSurface {
	s, ok := surfaceOf[*settingsSurface](m)
	if !ok {
		panic("the Settings card is not open")
	}
	return s
}

func (m *Model) settingsOpen() bool {
	_, ok := surfaceOf[*settingsSurface](m)
	return ok
}

// handleSettingsKey presses a key through the stack the way a terminal does.
func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) { return m.Update(msg) }

func (m *Model) viewSettings() string {
	return m.paintSettings(&m.settingsCard().state)
}

// closeSettings is the save-and-close a key or a click on the button asks.
func (m *Model) closeSettings() (tea.Model, tea.Cmd) {
	m.pop()
	return m, m.nextEffectCmd()
}

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

// esc, the save button and ctrl+c all close the card the same way, and the
// close queues the save once.
func TestSettingsEscAndCtrlCCloseAndSaveOnce(t *testing.T) {
	t.Parallel()
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
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

// A command being typed is a draft: ctrl+c and a stray click keep it.
func TestSettingsKeepsATypedEditorLine(t *testing.T) {
	t.Parallel()
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
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.settingsOpen() || m.effects.quitting {
		t.Fatalf("ctrl+c closed or quit with a command being typed, open %v", m.settingsOpen())
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
