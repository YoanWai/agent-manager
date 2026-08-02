package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultToolFallsBackWhenSettingStale(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting("default_tool", "deleted-tool"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if got := m.defaultTool(); got != "claude" {
		t.Fatalf("defaultTool = %q want claude (alphabetical fallback)", got)
	}
}

func TestDefaultSplitLayout(t *testing.T) {
	m := buildModel(t)
	if !m.defaultSplitLayout() {
		t.Fatal("split should be the default layout")
	}
	if err := m.store.SetSetting(diffLayoutSetting, "unified"); err != nil {
		t.Fatal(err)
	}
	if m.defaultSplitLayout() {
		t.Fatal("stored unified choice should opt out of split")
	}
}

func TestSettingsTogglesQuickClose(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.quickCloseSend {
		t.Fatal("settings should open on stay-open by default")
	}
	for i := 0; i < settingsFieldQuickClose; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldQuickClose {
		t.Fatalf("stepping down should reach the quick send field, got %d", m.settings.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.quickCloseAfterSend() {
		t.Fatal("close choice should persist after toggle")
	}
}

func TestSettingsTogglesReviewLayout(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settings.layoutSplit {
		t.Fatal("settings should open on split by default")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.settings.field != settingsFieldTheme {
		t.Fatalf("first down should focus theme field, got %d", m.settings.field)
	}
	for i := settingsFieldTheme; i < settingsFieldLayout; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldLayout {
		t.Fatalf("stepping down should reach the layout field, got %d", m.settings.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.defaultSplitLayout(); got {
		t.Fatal("layout should persist as unified after toggle")
	}
}

func TestSettingsWorktreeDefaultPersists(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	for m.settings.field != settingsFieldWorktree {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(worktreeSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
	}
	if !m.defaultWorktree() {
		t.Fatal("defaultWorktree should now report on")
	}
}

func TestSettingsShowsVersion(t *testing.T) {
	m := &Model{
		update:   updateInfo{version: "v0.9.0"},
		settings: settingsState{toolNames: []string{"claude"}},
	}
	out := m.viewSettings()
	if !strings.Contains(out, "version") || !strings.Contains(out, "v0.9.0") {
		t.Errorf("settings missing version: %q", out)
	}
	if strings.Contains(out, "available") {
		t.Errorf("no badge expected when up to date: %q", out)
	}
	m.update.latest = "v0.9.1"
	if out := m.viewSettings(); !strings.Contains(out, "v0.9.1") || !strings.Contains(out, "available") {
		t.Errorf("settings missing update badge: %q", out)
	}
}

func TestSettingsBugReportRowOpensIssue(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()
	if m.mode != modeSettings {
		t.Fatalf("settings should open, mode=%v", m.mode)
	}

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.settings.field = settingsFieldBugReport
	m.handleSettingsKey(key("enter"))
	if !strings.Contains(opened, "issues/new") {
		t.Fatalf("enter should open the issue page, got %q", opened)
	}
	if m.mode != modeSettings {
		t.Fatal("the action row must not close settings")
	}

	m.handleSettingsKey(key("esc"))
	if m.mode != modeList {
		t.Fatal("esc should still save and close")
	}
}

func TestSettingsCLIPickerHidesFromNewSessions(t *testing.T) {
	m := buildModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
		"grok":   {Command: "cat"},
	}}
	m.openSettings()
	m.settings.field = settingsFieldCLIs
	m.handleSettingsKey(key("enter"))
	if !m.settings.cliPicker {
		t.Fatal("enter on CLIs should open the picker")
	}
	out := m.viewSettings()
	for _, name := range []string{"claude", "codex", "grok"} {
		if !strings.Contains(out, name) {
			t.Fatalf("picker missing %q: %s", name, out)
		}
	}
	if !strings.Contains(out, "more will be supported soon") {
		t.Fatalf("picker missing support note: %s", out)
	}

	// Hide codex (index 1 in display order: claude, codex, grok).
	m.settings.cliCursor = 1
	m.handleSettingsKey(key(" "))
	if !m.settings.cliHidden["codex"] {
		t.Fatal("space should hide the focused CLI")
	}
	m.handleSettingsKey(key("esc"))
	if m.settings.cliPicker {
		t.Fatal("esc should leave the picker")
	}
	raw, err := m.store.Setting(hiddenToolsSetting)
	if err != nil || raw != "codex" {
		t.Fatalf("stored hidden_tools = %q err %v, want codex", raw, err)
	}

	enabled := m.enabledToolNames()
	for _, name := range enabled {
		if name == "codex" {
			t.Fatalf("codex should be omitted from create pickers: %v", enabled)
		}
	}
	m.openForm()
	if m.mode != modeForm {
		t.Fatalf("form should open with remaining CLIs, mode=%v err=%q", m.mode, m.errBar.text)
	}
	for _, name := range m.form.toolNames {
		if name == "codex" {
			t.Fatal("new-session form must not list a hidden CLI")
		}
	}
}

func TestSettingsCLIPickerKeepsOneEnabled(t *testing.T) {
	m := buildModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
	}}
	m.openSettings()
	m.openCLIPicker()
	m.settings.cliCursor = 0
	m.handleSettingsKey(key("enter"))
	if !m.settings.cliHidden["claude"] {
		t.Fatal("first hide should succeed")
	}
	// Only codex left; refusing further hides.
	m.settings.cliCursor = 1
	m.handleSettingsKey(key("enter"))
	if m.settings.cliHidden["codex"] {
		t.Fatal("must not hide the last enabled CLI")
	}
	if !strings.Contains(m.errBar.text, "at least one") {
		t.Fatalf("want keep-one message, got %q", m.errBar.text)
	}
}

func TestSettingsCLIRequestSupportOpensIssue(t *testing.T) {
	m := buildModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.openSettings()
	m.openCLIPicker()
	m.settings.cliCursor = len(m.settings.cliNames)
	m.handleSettingsKey(key("enter"))
	if !strings.Contains(opened, "issues/new") || !strings.Contains(opened, "enhancement") {
		t.Fatalf("request row should open a feature-request issue, got %q", opened)
	}
	if !m.settings.cliPicker {
		t.Fatal("opening the issue must leave the picker open")
	}
}

func TestCLIPickerShowsSupportAction(t *testing.T) {
	m := buildModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
	}}
	m.width = 80
	m.height = 40
	m.openSettings()
	m.openCLIPicker()
	m.settings.cliCursor = len(m.settings.cliNames)
	out := m.viewSettings()
	if !strings.Contains(out, "request CLI support") {
		t.Fatalf("missing request action:\n%s", out)
	}
	if !strings.Contains(out, "more will be supported soon") {
		t.Fatalf("missing support note:\n%s", out)
	}
}

func TestParseFormatHiddenTools(t *testing.T) {
	if got := parseHiddenTools(""); got != nil {
		t.Fatalf("empty parse = %v", got)
	}
	got := parseHiddenTools("codex, grok")
	if !got["codex"] || !got["grok"] || len(got) != 2 {
		t.Fatalf("parse = %v", got)
	}
	if formatHiddenTools(map[string]bool{"grok": true, "codex": true}) != "codex,grok" {
		t.Fatalf("format should sort: %q", formatHiddenTools(map[string]bool{"grok": true, "codex": true}))
	}
}
