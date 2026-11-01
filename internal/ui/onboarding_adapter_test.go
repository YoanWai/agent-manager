package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	onboardingruntime "github.com/YoanWai/agent-manager/internal/ui/onboarding/runtime"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func setupModel(t *testing.T) *Model {
	t.Helper()
	previous := uitheme.Current()
	t.Cleanup(func() { applyTheme(previous) })
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 36
	m.openSetup(true)
	probeSetup(t, m, map[string]bool{"claude": true, "codex": true})
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
		_, cmd := m.handleKey(key)
		applySetupCommand(m, cmd)
	}
}

func applySetupCommand(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		m.routeSetupMsg(cmd())
	}
}

func setupState(t *testing.T, m *Model) *uionboarding.Surface {
	t.Helper()
	s, ok := m.setupSurface()
	if !ok {
		t.Fatal("setup surface is not mounted")
	}
	return s
}

func probeSetup(t *testing.T, m *Model, installed map[string]bool) {
	t.Helper()
	s := setupState(t, m)
	m.applySetupProbe(onboardingruntime.ProbeRequest{Target: s.ID()}, onboardingruntime.ProbeResult{Installed: installed}, nil)
}

func clickSetup(t *testing.T, m *Model, text string) {
	t.Helper()
	x, y := paintedCell(t, m, text)
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
		_, cmd := m.handleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
		applySetupCommand(m, cmd)
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
		uisettings.ThemeSetting:         uitheme.Palettes[2].Name,
	} {
		if got := storedSetting(t, m, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	hidden := uisettings.ParseHidden(storedSetting(t, m, uisettings.HiddenToolsSetting))
	for _, name := range m.services.cfg.CLINames() {
		if want := name != "claude" && name != "codex"; hidden[name] != want {
			t.Errorf("%s hidden = %v, want %v", name, hidden[name], want)
		}
	}
	if !m.prefs.fullLayout || !m.prefs.comfortableRows || uitheme.Current().Name != uitheme.Palettes[2].Name {
		t.Fatalf("the running manager did not take the choices: full=%v comfortable=%v theme=%s",
			m.prefs.fullLayout, m.prefs.comfortableRows, uitheme.Current().Name)
	}
	if !m.setupOpen() || setupState(t, m).Step() != uionboarding.QuickPrompt {
		t.Fatalf("the setup moved on to %v in mode %v", setupState(t, m).Step(), m.screenID())
	}
}

// esc on the welcome asks, and leaving writes nothing.
func TestSkippingFromTheWelcomeWritesNothing(t *testing.T) {
	previous := uitheme.Current()
	t.Cleanup(func() { applyTheme(previous) })
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 36
	m.openSetup(true)
	pressKeys(m, escKey)
	if !m.setupOpen() || !strings.Contains(ansi.Strip(preparedView(m)), "Leave the setup?") {
		t.Fatal("esc did not ask")
	}
	clickSetup(t, m, "leave")
	m.drainEffects(t)
	if !m.listClear() {
		t.Fatalf("a click on leave left mode %v", m.screenID())
	}
	for _, key := range []string{uisettings.ThemeSetting, uisettings.SessionLayoutSetting, uisettings.ListDensitySetting, uisettings.HiddenToolsSetting} {
		if got := storedSetting(t, m, key); got != "" {
			t.Errorf("skipping wrote %s = %q", key, got)
		}
	}
}

// Cancelling the question on the theme step stays there, still previewing
// the palette under the cursor, and going back shows the saved one again.
func TestCancellingTheQuestionKeepsTheStep(t *testing.T) {
	m := setupModel(t)
	before := uitheme.Current().Name
	pressKeys(m, enterKey, enterKey, enterKey, downKey, downKey, escKey, escKey)
	if !m.setupOpen() || setupState(t, m).Step() != uionboarding.Theme || uitheme.Current().Name == before {
		t.Fatalf("esc twice on the theme step left %v in mode %v painting %s", setupState(t, m).Step(), m.screenID(), uitheme.Current().Name)
	}
	pressKeys(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if uitheme.Current().Name != before {
		t.Fatalf("going back kept the previewed palette %s", uitheme.Current().Name)
	}
	m.drainEffects(t)
	if got := storedSetting(t, m, uisettings.ThemeSetting); got != "" {
		t.Errorf("an unconfirmed theme was written: %q", got)
	}
}

// The guide names the arrow keys for focusing and coming back while the
// ←→ pair is on, and the enter and detach keys once it is off.
func TestSessionsCardNamesTheArrowKeys(t *testing.T) {
	m := setupModel(t)
	m.prefs.arrowStep = true
	if keys := m.setupKeys(); keys.Focus != "→" || keys.Back != "←" || keys.OtherFocus != "↵" || keys.OtherBack != "ctrl+q" {
		t.Fatalf("with the pair on: %+v", keys)
	}
	m.prefs.arrowStep = false
	if keys := m.setupKeys(); keys.Focus != "↵" || keys.Back != "ctrl+q" || keys.OtherFocus != "" || keys.OtherBack != "" {
		t.Fatalf("with the pair off: %+v", keys)
	}
}

func TestSettingsReopensTheSetup(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 36
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldSetup
	pressKeys(m, enterKey)
	if !m.setupOpen() {
		t.Fatalf("the setup row opened mode %v", m.screenID())
	}
	if setupState(t, m).Step() != uionboarding.Welcome {
		t.Fatalf("a reopened setup starts on %v", setupState(t, m).Step())
	}
}

func TestSetupTakesTheMouse(t *testing.T) {
	m := setupModel(t)
	if !strings.Contains(ansi.Strip(preparedView(m)), "[ ] quietchat") {
		t.Fatal("test setup: quietchat is not installed, so the first run leaves it unchecked")
	}
	m.prepareFrame()
	clickSetup(t, m, "quietchat")
	if !strings.Contains(ansi.Strip(preparedView(m)), "[✓] quietchat") {
		t.Fatalf("a click on quietchat left it unchecked:\n%s", ansi.Strip(m.View()))
	}
	clickSetup(t, m, "↵ next")
	if setupState(t, m).Step() != uionboarding.Layout {
		t.Fatalf("a click on ↵ next went to %v", setupState(t, m).Step())
	}
	wheelDown := tea.MouseMsg{X: 10, Y: 10, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	_, wheelCmd := m.handleMouse(wheelDown)
	applySetupCommand(m, wheelCmd)
	m.drainEffects(t)
	if !strings.Contains(ansi.Strip(preparedView(m)), "❯ ● Full screen") {
		t.Fatalf("the wheel did not move the layout choice:\n%s", ansi.Strip(m.View()))
	}
}

func TestSetupFillsTheTerminal(t *testing.T) {
	m := setupModel(t)
	for _, size := range [][2]int{{80, 24}, {120, 36}, {160, 45}, {200, 50}, {120, 8}, {200, 5}} {
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

func TestWelcomeAnimatesOnceTheLookupLands(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 36
	m.openSetup(true)
	probeSetup(t, m, map[string]bool{"claude": true})
	if setupState(t, m).Step() != uionboarding.Welcome || !setupState(t, m).Ticking() {
		t.Fatal("the welcome scene started no ticks")
	}
}

func TestSetupAnimatesOnlyTheGuide(t *testing.T) {
	m := setupModel(t)
	if setupState(t, m).Advance() != nil || setupState(t, m).Ticking() {
		t.Fatal("the welcome's ticks went on past the agents step")
	}
	if setupState(t, m).KeepAnimating() != nil {
		t.Fatal("the agents step asked for ticks")
	}
	_, cmd := m.applySetupOutcome(setupState(t, m).Model.Key(enterKey))
	for setupState(t, m).Step() != uionboarding.QuickPrompt {
		_, cmd = m.applySetupOutcome(setupState(t, m).Model.Key(enterKey))
	}
	if cmd == nil || !setupState(t, m).Ticking() {
		t.Fatal("reaching the quick prompt step started no ticks")
	}
	if setupState(t, m).KeepAnimating() != nil {
		t.Fatal("a second tick chain started while one runs")
	}
	if setupState(t, m).Advance() == nil {
		t.Fatal("a tick on an animated step did not ask for the next")
	}
	s, _ := m.setupSurface()
	m.Pop()
	_, stale, _ := m.routeSetupMsg(uionboarding.TickMsg{Surface: s})
	if stale != nil || s.Ticking() {
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
		for setupState(t, m).Step() != uionboarding.Import {
			for range 200 {
				lines := strings.Split(preparedView(m), "\n")
				if len(lines) != 36 {
					t.Fatalf("%v painted %d rows", setupState(t, m).Step(), len(lines))
				}
				for i, line := range lines {
					if width := ansi.StringWidth(line); width != 120 {
						t.Fatalf("%v row %d is %d wide:\n%s", setupState(t, m).Step(), i, width, ansi.Strip(line))
					}
				}
				setupState(t, m).Tick()
			}
			pressKeys(m, enterKey)
		}
	}
}

func TestSetupImportStartsTheImportAgent(t *testing.T) {
	m := setupModel(t)
	for setupState(t, m).Step() != uionboarding.Import {
		pressKeys(m, enterKey)
	}
	pressKeys(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, enterKey)
	if !m.listClear() {
		t.Fatalf("starting the import left mode %v", m.screenID())
	}
	var queued []uilaunch.Request
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.Request.(uilaunch.Request); ok {
			queued = append(queued, request)
		}
	}
	if len(queued) != 1 {
		t.Fatalf("queued %d spawns, want the import agent", len(queued))
	}
	request := queued[0]
	if request.Kind != uilaunch.Import || request.ToolName != "claude" || request.Name != importSessionName ||
		!strings.Contains(request.Prompt, "Bring my past claude conversations from the last 7 days") {
		t.Fatalf("queued %+v", request)
	}
}

func TestSetupImportKeepsTheRememberedSpawnPick(t *testing.T) {
	m := setupModel(t)
	m.ledger.spawner.RememberSpawn("codex", true)
	request := uilaunch.Request{Kind: uilaunch.Import, ToolName: "claude", Name: importSessionName}
	m.applySpawn(request, uilaunch.Result{Session: store.Session{ID: "import", Name: importSessionName, Tool: "claude"}}, nil)
	if m.ledger.spawner.LastSpawnTool != "codex" || !m.ledger.spawner.LastSpawnWorktree {
		t.Fatalf("the import replaced the remembered pick: %q worktree %v", m.ledger.spawner.LastSpawnTool, m.ledger.spawner.LastSpawnWorktree)
	}
}

func TestSetupFirstRunDefersProbeUntilInit(t *testing.T) {
	m := buildModel(t)
	if cmd := m.openSetup(true); cmd != nil {
		t.Fatal("first-run setup started work before the program's Init")
	}
	s := setupState(t, m)
	probeQueued := func() bool {
		for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
			if job == nil {
				continue
			}
			if request, ok := job.Request.(onboardingruntime.ProbeRequest); ok && request.Target == s.ID() {
				return true
			}
		}
		return false
	}
	if probeQueued() {
		t.Fatal("first-run setup queued a probe before Init")
	}
	m.Init()
	if !probeQueued() {
		t.Fatal("Init did not queue the first-run setup probe")
	}
}
