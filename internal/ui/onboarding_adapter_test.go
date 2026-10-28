package ui

import (
	"strings"
	"testing"

	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func setupModel(t *testing.T) *Model {
	t.Helper()
	previous := current()
	t.Cleanup(func() { applyTheme(previous) })
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 36
	m.openSetup(true)
	m.Update(setupProbedMsg{installed: map[string]bool{"claude": true, "codex": true}})
	pressKeys(m, enterKey)
	return m
}

func storedSetting(t *testing.T, m *Model, key string) string {
	t.Helper()
	value, err := m.services.store.Setting(key)
	if err != nil {
		t.Fatalf("reading %s: %v", key, err)
	}
	return value
}

func pressKeys(m *Model, keys ...tea.KeyMsg) {
	for _, key := range keys {
		m.Update(key)
	}
}

var (
	enterKey = tea.KeyMsg{Type: tea.KeyEnter}
	downKey  = tea.KeyMsg{Type: tea.KeyDown}
	escKey   = tea.KeyMsg{Type: tea.KeyEsc}
)

// Each confirmed step lands in the store under the Settings keys, and the
// running manager takes it at once.
func TestSetupWritesWhatSettingsWrites(t *testing.T) {
	m := setupModel(t)
	pressKeys(m, enterKey, downKey, enterKey, downKey, enterKey, downKey, downKey, enterKey)
	m.drainEffects(t)
	for key, want := range map[string]string{
		uisettings.SessionLayoutSetting: "full",
		uisettings.ListDensitySetting:   "comfortable",
		uisettings.ThemeSetting:         themes[2].Name,
	} {
		if got := storedSetting(t, m, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	hidden := uisettings.ParseHidden(storedSetting(t, m, uisettings.HiddenToolsSetting))
	for _, name := range m.services.cfg.AgentToolNames() {
		if want := name != "claude" && name != "codex"; hidden[name] != want {
			t.Errorf("%s hidden = %v, want %v", name, hidden[name], want)
		}
	}
	if !m.prefs.fullLayout || !m.prefs.comfortableRows || current().Name != themes[2].Name {
		t.Fatalf("the running manager did not take the choices: full=%v comfortable=%v theme=%s",
			m.prefs.fullLayout, m.prefs.comfortableRows, current().Name)
	}
	if m.mode != modeOnboarding || m.startup.setup.Step() != uionboarding.QuickPrompt {
		t.Fatalf("the setup moved on to %v in mode %v", m.startup.setup.Step(), m.mode)
	}
}

// Skipping on the theme step keeps the theme stored and paints with it
// again; nothing the setup did not confirm is written.
func TestSkippingTheSetupKeepsTheDefaults(t *testing.T) {
	m := setupModel(t)
	before := current().Name
	pressKeys(m, enterKey, enterKey, enterKey, downKey, downKey)
	if current().Name == before {
		t.Fatal("the theme step did not preview the palette under the cursor")
	}
	pressKeys(m, escKey)
	m.drainEffects(t)
	if m.mode != modeList {
		t.Fatalf("esc left mode %v", m.mode)
	}
	if current().Name != before {
		t.Fatalf("skipping kept the previewed palette %s", current().Name)
	}
	for _, key := range []string{uisettings.ThemeSetting, uisettings.SessionLayoutSetting, uisettings.ListDensitySetting} {
		if got := storedSetting(t, m, key); got != "" {
			t.Errorf("skipping wrote %s = %q", key, got)
		}
	}
}

func TestSettingsReopensTheSetup(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 36
	m.openSettings()
	m.settings.dialog.Field = uisettings.FieldSetup
	pressKeys(m, enterKey)
	if m.mode != modeOnboarding {
		t.Fatalf("the setup row opened mode %v", m.mode)
	}
	if m.startup.setup.Step() != uionboarding.Welcome {
		t.Fatalf("a reopened setup starts on %v", m.startup.setup.Step())
	}
}

func TestSetupTakesTheMouse(t *testing.T) {
	m := setupModel(t)
	if !strings.Contains(ansi.Strip(preparedView(m)), "[ ] command-code") {
		t.Fatal("test setup: command-code is not installed, so the first run leaves it unchecked")
	}
	m.prepareFrame()
	m, _ = clickBody(t, m, "command-code")
	if !strings.Contains(ansi.Strip(preparedView(m)), "[✓] command-code") {
		t.Fatalf("a click on command-code left it unchecked:\n%s", ansi.Strip(m.View()))
	}
	m = clickLabel(t, m, "↵ next")
	if m.startup.setup.Step() != uionboarding.Layout {
		t.Fatalf("a click on ↵ next went to %v", m.startup.setup.Step())
	}
	wheelDown := tea.MouseMsg{X: 10, Y: 10, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	m.Update(wheelDown)
	m.drainEffects(t)
	if !strings.Contains(ansi.Strip(preparedView(m)), "❯ ● Full screen") {
		t.Fatalf("the wheel did not move the layout choice:\n%s", ansi.Strip(m.View()))
	}
}

func TestSetupFillsTheTerminal(t *testing.T) {
	m := setupModel(t)
	for _, size := range [][2]int{{80, 24}, {120, 36}, {160, 45}, {200, 50}} {
		m.layout.width, m.layout.height = size[0], size[1]
		lines := strings.Split(preparedView(m), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d painted %d rows", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if width := ansi.StringWidth(line); width != size[0] {
				t.Fatalf("%dx%d row %d is %d wide", size[0], size[1], i, width)
			}
		}
	}
}

func TestSetupAnimatesOnlyTheGuide(t *testing.T) {
	m := setupModel(t)
	if m.keepSetupAnimating() != nil {
		t.Fatal("the agents step asked for ticks")
	}
	_, cmd := m.applySetupOutcome(m.startup.setup.Key(enterKey))
	for m.startup.setup.Step() != uionboarding.QuickPrompt {
		_, cmd = m.applySetupOutcome(m.startup.setup.Key(enterKey))
	}
	if cmd == nil || !m.startup.setup.ticking {
		t.Fatal("reaching the quick prompt step started no ticks")
	}
	if m.keepSetupAnimating() != nil {
		t.Fatal("a second tick chain started while one runs")
	}
	if m.applySetupTick() == nil {
		t.Fatal("a tick on an animated step did not ask for the next")
	}
	m.mode = modeList
	if m.applySetupTick() != nil || m.startup.setup.ticking {
		t.Fatal("ticks kept coming after the setup closed")
	}
}

// Every beat of every guide scene fits the window it is drawn in, in both
// layouts.
func TestSetupScenesFitTheTerminal(t *testing.T) {
	for _, full := range []bool{false, true} {
		m := setupModel(t)
		m.layout.width, m.layout.height = 120, 36
		if full {
			pressKeys(m, enterKey, downKey)
		} else {
			pressKeys(m, enterKey)
		}
		pressKeys(m, enterKey, enterKey, enterKey)
		for m.startup.setup.Step() != uionboarding.Places {
			for range 140 {
				lines := strings.Split(preparedView(m), "\n")
				if len(lines) != 36 {
					t.Fatalf("%v painted %d rows", m.startup.setup.Step(), len(lines))
				}
				for i, line := range lines {
					if width := ansi.StringWidth(line); width != 120 {
						t.Fatalf("%v row %d is %d wide:\n%s", m.startup.setup.Step(), i, width, ansi.Strip(line))
					}
				}
				m.startup.setup.Tick()
			}
			pressKeys(m, enterKey)
		}
	}
}
