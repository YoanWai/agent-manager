package ui

import (
	"strings"
	"testing"

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

func TestQuickCloseAfterSendDefaultsToStayingOpen(t *testing.T) {
	m := buildModel(t)
	if m.quickCloseAfterSend() {
		t.Fatal("quick bar should stay open by default")
	}
	if err := m.store.SetSetting(quickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}
	if !m.quickCloseAfterSend() {
		t.Fatal("stored close choice should opt in")
	}
}

func TestQuickPromptClosesAfterSendWhenEnabled(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	if err := m.store.SetSetting(quickCloseSetting, "close"); err != nil {
		t.Fatal(err)
	}

	m.openQuickMode()
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	if m.quick.active {
		t.Fatal("quick mode should close after a send when the setting is on")
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
