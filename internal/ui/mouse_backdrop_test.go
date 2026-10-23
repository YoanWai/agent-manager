package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A corner of the terminal is backdrop under every centered card.
func clickBackdrop(t *testing.T, m *Model) *Model {
	t.Helper()
	preparedView(m)
	if m.layout.card.holds(0, 0) {
		t.Fatalf("test setup: the card covers the corner: %+v", m.layout.card)
	}
	m = leftClick(m, 0, 0)
	m.drainEffects(t)
	return m
}

func TestBackdropClickClosesACardTheWayEscDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		open func(*Model)
	}{
		{"help", func(m *Model) { m.openHelp() }},
		{"messages", func(m *Model) { m.openNotices("") }},
		{"settings", func(m *Model) { m.applyCmd(t, m.openSettings()) }},
		{"confirm", func(m *Model) {
			m.openConfirm(confirmTarget{action: actionKill, label: "kill alpha? frees its RAM."})
		}},
		{"move", func(m *Model) { m.openMove() }},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 120, 34
		createSession(t, m, "alpha", t.TempDir(), "")
		m.selectSessionRow(t, "alpha")
		tc.open(m)
		if m.listClear() {
			t.Fatalf("%s: test setup did not open the card, err %q", tc.name, m.errBar.text)
		}
		if m = clickBackdrop(t, m); !m.listClear() {
			t.Errorf("%s: a backdrop click should close the card, mode = %v", tc.name, m.mode)
		}
		if confirmOpen(m) {
			t.Errorf("%s: a backdrop click on a confirm is its cancel, target = %+v", tc.name, pendingConfirm(m))
		}
	}
}

func TestBackdropClickKeepsATypedDraft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		open func(*Model)
	}{
		{"new session", func(m *Model) { m.applyCmd(t, m.openForm()) }},
		{"new group", func(m *Model) { m.openGroupForm() }},
		{"fork", func(m *Model) { m.push(newForkSurface(store.Session{Name: "alpha"}, 1)) }},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 120, 34
		createSessionOn(t, m, "alpha", "claude", t.TempDir())
		m.selectSessionRow(t, "alpha")
		tc.open(m)
		if len(m.surfaces) != 1 {
			t.Fatalf("%s: test setup put %d surfaces up, err %q", tc.name, len(m.surfaces), m.errBar.text)
		}
		if m = clickBackdrop(t, m); len(m.surfaces) != 1 {
			t.Errorf("%s: a stray click closed the draft", tc.name)
		}
	}
}

func TestBackdropClickKeepsTheSettingsEditorField(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	editorSettings(t, m, "code")
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyLeft})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().state.Editor.Typing() {
		t.Fatal("test setup: enter on custom opens the field")
	}
	if m = clickBackdrop(t, m); !m.settingsOpen() || !m.settingsCard().state.Editor.Typing() {
		t.Fatalf("a stray click should keep the typed line open, mode = %v typing = %v", m.mode, m.settingsCard().state.Editor.Typing())
	}
}

// A click inside the card that lands on nothing it paints leaves it open.
func TestClickOnACardsOwnBlankStaysOpen(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	preparedView(m)
	if m = leftClick(m, m.layout.card.left+1, m.layout.card.top+1); !helpOpen(m) {
		t.Fatalf("a click on the card's own edge should leave it open, mode = %v", m.mode)
	}
}

func TestBackdropClickKeepsASavedPromptDraft(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.applyCmd(t, m.openSettings())
	m.applyCmd(t, m.runSettingsResult(&m.settingsCard().state, m.settingsCard().state.OpenPrompts()))
	m.pressInSettings(t, runeKey("n"))
	if !m.settingsCard().state.Prompts.Editing() {
		t.Fatal("test setup: n opens the prompt editor")
	}
	if m = clickBackdrop(t, m); !m.settingsOpen() || !m.settingsCard().state.Prompts.Editing() {
		t.Fatalf("a stray click should keep the prompt being typed, mode = %v", m.mode)
	}
}

// The saved prompts card over a focused session covers the focus footer,
// so only the card's own keys answer a click.
func TestSavedPromptsOverFocusHideTheFooterKeys(t *testing.T) {
	m := focusedModel(t)
	m.openFocusSaved()
	preparedView(m)
	if len(m.layout.keyHits) == 0 {
		t.Fatal("test setup: the card paints its own keys")
	}
	for _, hit := range m.layout.keyHits {
		if !m.layout.card.holds(hit.x0, hit.y) {
			t.Fatalf("a key at %d,%d answers outside the card %+v", hit.x0, hit.y, m.layout.card)
		}
	}
}

// A rename typed into its row holds the list still under the mouse.
func TestRenameHoldsTheListUnderTheMouse(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.openRename()
	x, y := bodyCell(t, m, "beta")
	m = leftClick(m, x, y)
	m = wheel(m, tea.MouseButtonWheelDown, x, y)
	if row, ok := m.selectedRow(); !renameOpen(m) || !ok || row.sess.Name != "alpha" {
		t.Fatalf("the mouse moved the list under a rename: mode %v row %+v", m.mode, row.sess.Name)
	}
}

// A card answers a click when the button comes back up over the spot the
// press landed on, so a press on the backdrop released over the card
// closes nothing.
func TestACardClickRunsOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	preparedView(m)
	card := m.layout.card
	if m = leftPress(m, 0, 0); !helpOpen(m) {
		t.Fatalf("the press alone closed the card, mode = %v", m.mode)
	}
	if m = leftRelease(m, card.left+1, card.top+1); !helpOpen(m) {
		t.Fatalf("a release over the card closed it, mode = %v", m.mode)
	}
	if m = leftClick(m, 0, 0); m.mode != modeList {
		t.Fatalf("a click on the backdrop should close the card, mode = %v", m.mode)
	}
}
