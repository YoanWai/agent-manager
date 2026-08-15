package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestSettingsNotificationsPersist(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settings.notifications {
		t.Fatal("notifications should open on by default")
	}
	if m.settings.notifyFinished {
		t.Fatal("notify on finish should open off by default")
	}
	for m.settings.field != settingsFieldNotify {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(notificationsSetting); err != nil || chosen != "off" {
		t.Fatalf("want stored off, got %q err %v", chosen, err)
	}
	if chosen, err := m.store.Setting(notifyFinishedSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
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
	if strings.Contains(out, "update to") {
		t.Errorf("no update action expected when up to date: %q", out)
	}
	m.settings.field = settingsFieldUpdate
	out = m.viewSettings()
	if !strings.Contains(out, keyCap("↵/esc", "save")) {
		t.Errorf("current version row should hint save, not update: %q", out)
	}
	m.settings.field = 0
	m.update.latest = "v0.9.1"
	out = m.viewSettings()
	if !strings.Contains(out, "v0.9.1") || !strings.Contains(out, "update to") {
		t.Errorf("settings missing update action: %q", out)
	}
	m.settings.field = settingsFieldUpdate
	out = m.viewSettings()
	if !strings.Contains(out, keyStyle.Render("↵")+mutedStyle.Render(" update to")) {
		t.Errorf("focused update row should hint enter: %q", out)
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
	_, cmd := m.handleSettingsKey(key("enter"))
	if opened != "" {
		t.Fatal("browser started during Update")
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(opened, "template=bug_report.yml") {
		t.Fatalf("enter should open the bug form, got %q", opened)
	}
	if !strings.Contains(opened, "version=") {
		t.Fatalf("bug form should carry the version, got %q", opened)
	}
	if m.mode != modeSettings {
		t.Fatal("the action row must not close settings")
	}

	m.handleSettingsKey(key("esc"))
	if m.mode != modeList {
		t.Fatal("esc should still save and close")
	}
}

func TestSettingsFeatureRequestRowOpensForm(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.settings.field = settingsFieldFeatureRequest
	_, cmd := m.handleSettingsKey(key("enter"))
	m.applyCmd(t, cmd)
	if !strings.Contains(opened, "template=feature_request.yml") {
		t.Fatalf("enter should open the feature form, got %q", opened)
	}
	if strings.Contains(opened, "template=bug_report.yml") {
		t.Fatalf("suggest a change opened the bug form: %q", opened)
	}
	if m.mode != modeSettings {
		t.Fatal("the action row must not close settings")
	}
}

func TestSettingsBugReportRowIsHighlighted(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()
	// Keep focus off the CTA rows so the accent2 unfocused styling is what paints.
	m.settings.field = settingsFieldTool
	card := ansi.Strip(m.viewSettings())
	if !strings.Contains(card, "report a bug") {
		t.Fatalf("settings should show the bug report row: %q", card)
	}
	if !strings.Contains(card, "suggest a change") {
		t.Fatalf("settings should show the feature request row: %q", card)
	}
	// Unfocused labels use accent2 + bold; prove that SGR lands on each label.
	styled := m.viewSettings()
	bugLabel := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render("report a bug")
	if !strings.Contains(styled, bugLabel) {
		t.Fatalf("unfocused bug report label should use accent2 bold styling")
	}
	ideaLabel := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render("suggest a change")
	if !strings.Contains(styled, ideaLabel) {
		t.Fatalf("unfocused feature request label should use accent2 bold styling")
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
	_, cmd := m.handleSettingsKey(key("enter"))
	if opened != "" {
		t.Fatal("browser started during Update")
	}
	m.applyCmd(t, cmd)
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

func TestSettingsUpdateRowAppliesOnEnter(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.2.0"
	m.update.latest = "v0.3.0"
	m.openSettings()
	m.settings.field = settingsFieldUpdate

	applied := ""
	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest: "v0.6.0",
			URL:    "https://github.com/YoanWai/agent-manager/releases/tag/v0.6.0",
			Releases: []update.Release{
				uiRelease("v0.6.0", "Newest release"),
				uiRelease("v0.2.0", "Current release"),
			},
		}, nil
	}
	applyUpdate = func(_ context.Context, tag, execPath string) error {
		applied = tag + " " + execPath
		return nil
	}

	_, cmd := m.handleSettingsKey(key("enter"))
	if cmd == nil {
		t.Fatal("enter on the update row should start the update")
	}
	if !m.update.applying {
		t.Fatal("applying should be marked while the download runs")
	}
	if m.mode != modeSettings {
		t.Fatal("starting the update must keep settings open")
	}
	msg, ok := cmd().(updateAppliedMsg)
	if !ok {
		t.Fatalf("cmd returned %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("apply: %v", msg.err)
	}
	if !strings.HasPrefix(applied, "v0.6.0 ") {
		t.Fatalf("applyUpdate saw %q", applied)
	}
	updated, quit := m.Update(msg)
	m = updated.(*Model)
	if m.update.applying {
		t.Fatal("applying should clear once the swap lands")
	}
	if m.RestartPath() == "" {
		t.Fatal("a successful swap must set the restart path")
	}
	if quit == nil {
		t.Fatal("a successful swap must quit so main can exec the new build")
	}
}

func TestSettingsUpdateRowIdleWhenCurrent(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.3.0"
	m.openSettings()
	m.settings.field = settingsFieldUpdate

	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	called := false
	applyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	_, cmd := m.handleSettingsKey(key("enter"))
	if called || cmd != nil || m.update.applying {
		t.Fatal("enter on version when up to date must not start an update")
	}
	if m.mode != modeList {
		t.Fatal("enter with no update should still save and close")
	}
}

func TestSettingsUpdateRowApplyFailureSurfaces(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.2.0"
	m.update.latest = "v0.3.0"
	m.openSettings()
	m.settings.field = settingsFieldUpdate

	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest:   "v0.3.0",
			URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
			Releases: []update.Release{uiRelease("v0.3.0", "Newest release"), uiRelease("v0.2.0", "Current release")},
		}, nil
	}
	applyUpdate = func(context.Context, string, string) error {
		return errors.New("permission denied")
	}

	_, cmd := m.handleSettingsKey(key("enter"))
	updated, _ := m.Update(cmd().(updateAppliedMsg))
	m = updated.(*Model)
	if m.update.applying {
		t.Fatal("applying should clear on failure")
	}
	if m.RestartPath() != "" {
		t.Fatal("a failed swap must not restart")
	}
	if !strings.Contains(m.errBar.text, "permission denied") {
		t.Fatalf("failure should surface, err=%q", m.errBar.text)
	}
}

func TestSettingsUpdateRowPersistsStagedSettings(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.2.0"
	m.update.latest = "v0.3.0"
	m.openSettings()
	m.settings.themeIndex = (m.settings.themeIndex + 1) % len(themes)
	staged := themes[m.settings.themeIndex].Name
	m.settings.field = settingsFieldUpdate

	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest:   "v0.3.0",
			URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
			Releases: []update.Release{uiRelease("v0.3.0", "Newest release"), uiRelease("v0.2.0", "Current release")},
		}, nil
	}
	applyUpdate = func(context.Context, string, string) error { return nil }

	if _, cmd := m.handleSettingsKey(key("enter")); cmd == nil {
		t.Fatal("enter on the update row should start the update")
	}
	got, err := m.store.Setting(themeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("theme setting = %q, want staged %q persisted before the restart", got, staged)
	}
}

func TestSettingsHasNoTerminalRows(t *testing.T) {
	m := buildModel(t)
	if strings.Contains(ansi.Strip(m.viewSettings()), "terminal rows") {
		t.Fatal("terminal rows setting must be gone")
	}
}
