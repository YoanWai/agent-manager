package ui

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/YoanWai/agent-manager/internal/update"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestSettingsTogglesQuickClose(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settingsCard().State().QuickCloseSend {
		t.Fatal("settings should open on stay-open by default")
	}
	for i := 0; i < uisettings.FieldQuickClose; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsCard().State().Field != uisettings.FieldQuickClose {
		t.Fatalf("stepping down should reach the quick send field, got %d", m.settingsCard().State().Field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(uisettings.QuickCloseSetting); err != nil || chosen != "close" {
		t.Fatalf("close choice = %q err %v, want persisted close", chosen, err)
	}
}

func TestSettingsTogglesReviewLayout(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settingsCard().State().LayoutSplit {
		t.Fatal("settings should open on split by default")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.settingsCard().State().Field != uisettings.FieldTheme {
		t.Fatalf("first down should focus theme field, got %d", m.settingsCard().State().Field)
	}
	for i := uisettings.FieldTheme; i < uisettings.FieldLayout; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsCard().State().Field != uisettings.FieldLayout {
		t.Fatalf("stepping down should reach the layout field, got %d", m.settingsCard().State().Field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if got := m.defaultSplitLayout(); got {
		t.Fatal("layout should persist as unified after toggle")
	}
}

func TestSettingsWorktreeDefaultPersists(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	for m.settingsCard().State().Field != uisettings.FieldWorktree {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(uisettings.WorktreeSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
	}
}

func TestSettingsCoordinationBriefsTheNextSpawn(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settingsCard().State().Proactive {
		t.Fatal("coordination should open on request by default")
	}
	if !strings.Contains(ansi.Strip(m.viewSettings()), "coordination") || !strings.Contains(ansi.Strip(m.viewSettings()), "on request") {
		t.Fatalf("settings do not show the coordination row:\n%s", ansi.Strip(m.viewSettings()))
	}
	for m.settingsCard().State().Field != uisettings.FieldCoordination {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if !strings.Contains(ansi.Strip(m.viewSettings()), "proactive") {
		t.Fatalf("the stepped row does not read proactive:\n%s", ansi.Strip(m.viewSettings()))
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if proactive, err := m.services.store.ProactiveCoordination(); err != nil || !proactive {
		t.Fatalf("want proactive stored, got %v err %v", proactive, err)
	}

	// ready-tool has no MCP client, so the mode reaches it as the note its
	// first prompt opens with.
	if err := m.spawnSession("ready-tool", "api-build", t.TempDir(), "", "build the api", false, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.services.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sess.LaunchPrompt, launch.ProactiveCoordinationNote) {
		t.Fatalf("a spawn after choosing proactive launched with %q", sess.LaunchPrompt)
	}
}

func TestSettingsNotificationsPersist(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settingsCard().State().Notifications {
		t.Fatal("notifications should open on by default")
	}
	if m.settingsCard().State().NotifyFinished {
		t.Fatal("notify on finish should open off by default")
	}
	for m.settingsCard().State().Field != uisettings.FieldNotify {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if chosen, err := m.services.store.Setting(uisettings.NotificationsSetting); err != nil || chosen != "off" {
		t.Fatalf("want stored off, got %q err %v", chosen, err)
	}
	if chosen, err := m.services.store.Setting(uisettings.NotifyFinishedSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
	}
}

func TestSettingsMouseTogglePersists(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settingsCard().State().MouseDisabled {
		t.Fatal("mouse should open on by default")
	}
	for m.settingsCard().State().Field != uisettings.FieldMouse {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(uisettings.MouseSetting); err != nil || chosen != "off" {
		t.Fatalf("want stored off, got %q err %v", chosen, err)
	}
	if !m.prefs.mouseDisabled {
		t.Fatal("model should carry the toggled value after save")
	}
	if loaded := reloadModel(t, m); !loaded.prefs.mouseDisabled {
		t.Fatal("a fresh model should reload the persisted choice")
	}
}

func TestSettingsShowsVersion(t *testing.T) {
	m := &Model{
		layout:  layoutState{width: 120, height: 60},
		release: uirelease.State{Version: "v0.9.0"},
	}
	m.push(newSettingsSurface(m, uisettings.State{ToolNames: []string{"claude"}}))
	out := m.viewSettings()
	if !strings.Contains(out, "version") || !strings.Contains(out, "v0.9.0") {
		t.Errorf("settings missing version: %q", out)
	}
	if strings.Contains(out, "update to") {
		t.Errorf("no update action expected when up to date: %q", out)
	}
	m.settingsCard().State().Field = uisettings.FieldUpdate
	out = m.viewSettings()
	if !strings.Contains(out, uitheme.KeyCap("↵/esc", "save and close")) {
		t.Errorf("current version row should hint save, not update: %q", out)
	}
	m.settingsCard().State().Field = 0
	m.release.Latest = "v0.9.1"
	out = m.viewSettings()
	if !strings.Contains(out, "v0.9.1") || !strings.Contains(out, "update to") {
		t.Errorf("settings missing update action: %q", out)
	}
	m.settingsCard().State().Field = uisettings.FieldUpdate
	out = m.viewSettings()
	if !strings.Contains(out, uitheme.KeyStyle().Render("↵")+uitheme.MutedStyle().Render(" update to")) {
		t.Errorf("focused update row should hint enter: %q", out)
	}
}

func TestSettingsDocsRowOpensDocs(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	m.settingsCard().State().Field = uisettings.FieldDocs
	_, cmd := m.handleSettingsKey(key("enter"))
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("enter should open the docs, got %q", opened)
	}
	if !m.settingsOpen() {
		t.Fatal("the docs row must not close settings")
	}

	opened = ""
	m.layout.width, m.layout.height = 120, 60
	frame := preparedView(m)
	if !strings.Contains(frame, uitheme.HintCap("↵", "open the docs")) {
		t.Fatal("the docs shortcut should wear the footer badge")
	}
	m, cmd = clickBody(t, m, "open the docs")
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("click should open the docs, got %q", opened)
	}

	opened = ""
	x, y := paintedCell(t, m, "save and close")
	m, cmd = leftClickCmd(m, x, y)
	m.applyCmd(t, cmd)
	if opened != "" {
		t.Fatalf("the footer save opened %q", opened)
	}
	if m.settingsOpen() {
		t.Fatal("the footer save and close should leave settings")
	}
}

func TestSettingsBugReportRowOpensIssue(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()
	if !m.settingsOpen() {
		t.Fatalf("settings should open, screen = %v", m.screenID())
	}

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	m.settingsCard().State().Field = uisettings.FieldBugReport
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
	if !m.settingsOpen() {
		t.Fatal("the action row must not close settings")
	}

	m.handleSettingsKey(key("esc"))
	if m.settingsOpen() {
		t.Fatal("esc should still save and close")
	}
}

func TestSettingsFeatureRequestRowOpensForm(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	m.settingsCard().State().Field = uisettings.FieldFeatureRequest
	_, cmd := m.handleSettingsKey(key("enter"))
	m.applyCmd(t, cmd)
	if !strings.Contains(opened, "template=feature_request.yml") {
		t.Fatalf("enter should open the feature form, got %q", opened)
	}
	if strings.Contains(opened, "template=bug_report.yml") {
		t.Fatalf("suggest a change opened the bug form: %q", opened)
	}
	if !m.settingsOpen() {
		t.Fatal("the action row must not close settings")
	}
}

func TestSettingsBugReportRowIsHighlighted(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 120, 60
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()
	// Keep focus off the CTA rows so the accent2 unfocused styling is what paints.
	m.settingsCard().State().Field = uisettings.FieldTool
	card := ansi.Strip(m.viewSettings())
	if !strings.Contains(card, "report a bug") {
		t.Fatalf("settings should show the bug report row: %q", card)
	}
	if !strings.Contains(card, "suggest a change") {
		t.Fatalf("settings should show the feature request row: %q", card)
	}
	// Unfocused labels use accent2 + bold; prove that SGR lands on each label.
	styled := m.viewSettings()
	bugLabel := lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Bold(true).Render("report a bug")
	if !strings.Contains(styled, bugLabel) {
		t.Fatalf("unfocused bug report label should use accent2 bold styling")
	}
	ideaLabel := lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Bold(true).Render("suggest a change")
	if !strings.Contains(styled, ideaLabel) {
		t.Fatalf("unfocused feature request label should use accent2 bold styling")
	}
}

func TestSettingsCLIPickerHidesFromNewSessions(t *testing.T) {
	m := buildModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
		"grok":   {Command: "cat"},
	}}
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldCLIs
	m.handleSettingsKey(key("enter"))
	if !m.settingsCard().State().CLIPicker {
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
	m.settingsCard().State().CLICursor = 1
	m.handleSettingsKey(key(" "))
	if !m.settingsCard().State().CLIHidden["codex"] {
		t.Fatal("space should hide the focused CLI")
	}
	m.handleSettingsKey(key("esc"))
	m.drainEffects(t)
	if m.settingsCard().State().CLIPicker {
		t.Fatal("esc should leave the picker")
	}
	hidden, err := m.services.store.HiddenTools()
	if err != nil || len(hidden) != 1 || !hidden["codex"] {
		t.Fatalf("stored hidden tools = %v err %v, want codex", hidden, err)
	}

	enabled := m.cachedEnabledToolNames()
	for _, name := range enabled {
		if name == "codex" {
			t.Fatalf("codex should be omitted from create pickers: %v", enabled)
		}
	}
	m.openForm()
	if !formOpen(m) {
		t.Fatalf("form should open with remaining CLIs, screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	for _, name := range formTools(m) {
		if name == "codex" {
			t.Fatal("new-session form must not list a hidden CLI")
		}
	}
}

func TestSettingsCLIPickerKeepsOneEnabled(t *testing.T) {
	m := buildModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
	}}
	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 0
	m.handleSettingsKey(key("enter"))
	if !m.settingsCard().State().CLIHidden["claude"] {
		t.Fatal("first hide should succeed")
	}
	// Only codex left; refusing further hides.
	m.settingsCard().State().CLICursor = 1
	m.handleSettingsKey(key("enter"))
	if m.settingsCard().State().CLIHidden["codex"] {
		t.Fatal("must not hide the last enabled CLI")
	}
	if !strings.Contains(m.errBar.text, "at least one") {
		t.Fatalf("want keep-one message, got %q", m.errBar.text)
	}
}

func TestSettingsCLIRequestSupportOpensIssue(t *testing.T) {
	m := buildModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = len(m.settingsCard().State().CLINames)
	_, cmd := m.handleSettingsKey(key("enter"))
	if opened != "" {
		t.Fatal("browser started during Update")
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(opened, "issues/new") || !strings.Contains(opened, "support+another+coding+CLI") {
		t.Fatalf("request row should open a feature-request issue, got %q", opened)
	}
	if !m.settingsCard().State().CLIPicker {
		t.Fatal("opening the issue must leave the picker open")
	}
}

func TestCLIPickerShowsSupportAction(t *testing.T) {
	m := buildModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
	}}
	m.layout.width = 80
	m.layout.height = 40
	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = len(m.settingsCard().State().CLINames)
	out := m.viewSettings()
	if !strings.Contains(out, "request CLI support") {
		t.Fatalf("missing request action:\n%s", out)
	}
	if !strings.Contains(out, "more will be supported soon") {
		t.Fatalf("missing support note:\n%s", out)
	}
}

func TestSettingsUpdateRowAppliesOnEnter(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.release.Version = "v0.2.0"
	m.release.Latest = "v0.3.0"
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldUpdate

	applied := ""
	orig := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = orig }()
	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest: "v0.6.0",
			URL:    "https://github.com/YoanWai/agent-manager/releases/tag/v0.6.0",
			Releases: []update.Release{
				uiRelease("v0.6.0", "Newest release"),
				uiRelease("v0.2.0", "Current release"),
			},
		}, nil
	}
	uirelease.ApplyUpdate = func(_ context.Context, tag, execPath string) error {
		applied = tag + " " + execPath
		return nil
	}

	_, cmd := m.handleSettingsKey(key("enter"))
	if cmd == nil {
		t.Fatal("enter on the update row should start the update")
	}
	if !m.release.Applying {
		t.Fatal("applying should be marked while the download runs")
	}
	if !m.settingsOpen() {
		t.Fatal("starting the update must keep settings open")
	}
	completed := cmd().(uieffects.Completed)
	if completed.Err != nil {
		t.Fatalf("staged settings save: %v", completed.Err)
	}
	updated, next := m.Update(completed)
	m = updated.(*Model)
	if next == nil {
		t.Fatal("no update command after the save completed")
	}
	var msg tea.Msg
	switch batch := next().(type) {
	case tea.BatchMsg:
		for _, c := range batch {
			if c != nil {
				msg = c()
				break
			}
		}
	default:
		msg = next()
	}
	swap, ok := msg.(uirelease.AppliedMsg)
	if !ok {
		t.Fatalf("update command returned %T", msg)
	}
	if swap.Err != nil {
		t.Fatalf("apply: %v", swap.Err)
	}
	if !strings.HasPrefix(applied, "v0.6.0 ") {
		t.Fatalf("uirelease.ApplyUpdate saw %q", applied)
	}
	updated, quit := m.Update(swap)
	m = updated.(*Model)
	if m.release.Applying {
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.release.Version = "v0.3.0"
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldUpdate

	orig := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = orig }()
	called := false
	uirelease.ApplyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	m.handleSettingsKey(key("enter"))
	m.drainEffects(t)
	if called || m.release.Applying {
		t.Fatal("enter on version when up to date must not start an update")
	}
	if m.settingsOpen() {
		t.Fatal("enter with no update should still save and close")
	}
}

func TestSettingsUpdateRowApplyFailureSurfaces(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.release.Version = "v0.2.0"
	m.release.Latest = "v0.3.0"
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldUpdate

	orig := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = orig }()
	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest:   "v0.3.0",
			URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
			Releases: []update.Release{uiRelease("v0.3.0", "Newest release"), uiRelease("v0.2.0", "Current release")},
		}, nil
	}
	uirelease.ApplyUpdate = func(context.Context, string, string) error {
		return errors.New("permission denied")
	}

	_, cmd := m.handleSettingsKey(key("enter"))
	completed := cmd().(uieffects.Completed)
	if completed.Err != nil {
		t.Fatalf("staged settings save: %v", completed.Err)
	}
	updated, next := m.Update(completed)
	m = updated.(*Model)
	if next == nil {
		t.Fatal("no update command after the save completed")
	}
	swap, ok := next().(uirelease.AppliedMsg)
	if !ok {
		t.Fatalf("update command returned %T", next())
	}
	updated, _ = m.Update(swap)
	m = updated.(*Model)
	if m.release.Applying {
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.release.Version = "v0.2.0"
	m.release.Latest = "v0.3.0"
	m.openSettings()
	m.settingsCard().State().ThemeIndex = (m.settingsCard().State().ThemeIndex + 1) % len(uitheme.Palettes)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settingsCard().State().Field = uisettings.FieldUpdate

	orig := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = orig }()
	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest:   "v0.3.0",
			URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
			Releases: []update.Release{uiRelease("v0.3.0", "Newest release"), uiRelease("v0.2.0", "Current release")},
		}, nil
	}
	uirelease.ApplyUpdate = func(context.Context, string, string) error { return nil }

	_, cmd := m.handleSettingsKey(key("enter"))
	if cmd == nil {
		t.Fatal("enter on the update row should start the update")
	}
	m.applyCmd(t, cmd)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("theme setting = %q, want staged %q persisted before the restart", got, staged)
	}
}

func TestSettingsHasNoTerminalRows(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if strings.Contains(ansi.Strip(m.viewSettings()), "terminal rows") {
		t.Fatal("terminal rows setting must be gone")
	}
}

// otherWriterTheme is a theme this manager would not write on its own.
func otherWriterTheme(t *testing.T) string {
	t.Helper()
	for _, theme := range uitheme.Palettes {
		if theme.Name != uitheme.Current().Name {
			return theme.Name
		}
	}
	t.Fatal("need a second theme")
	return ""
}

// Another manager or the CLI may change a key after this manager's cache
// loaded; a save must not write the stale cached value back over it.
func TestSettingsSaveKeepsOtherWritersKeys(t *testing.T) {
	for _, tc := range []struct {
		name      string
		applyLoad bool
	}{
		{name: "save before the fresh load lands"},
		{name: "fresh load discarded by the staged edit", applyLoad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			load := m.openSettings()
			m.settingsCard().State().Field = uisettings.FieldWorktree
			m.cycleSetting(1)

			theirs := otherWriterTheme(t)
			if err := m.services.store.SetSetting(uisettings.ThemeSetting, theirs); err != nil {
				t.Fatal(err)
			}
			if err := m.services.store.SetProactiveCoordination(true); err != nil {
				t.Fatal(err)
			}
			if tc.applyLoad {
				m.applyTestMsg(t, load())
			}
			_, save := m.handleSettingsKey(key("enter"))
			m.applyCmd(t, save)

			if got, err := m.services.store.Setting(uisettings.WorktreeSetting); err != nil || got != "on" {
				t.Fatalf("worktree=%q err %v, want the changed value on", got, err)
			}
			if got, err := m.services.store.Setting(uisettings.ThemeSetting); err != nil || got != theirs {
				t.Fatalf("theme=%q err %v, want the other writer's %q", got, err, theirs)
			}
			if proactive, err := m.services.store.ProactiveCoordination(); err != nil || !proactive {
				t.Fatalf("coordination proactive=%t err %v, want the other writer's proactive", proactive, err)
			}
		})
	}
}

func TestSettingsEscWithoutChangesWritesNothing(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	_, save := m.handleSettingsKey(key("esc"))
	if m.settingsOpen() {
		t.Fatalf("esc should close the dialog, screen = %v", m.screenID())
	}
	m.applyCmd(t, save)

	values, hidden, hiddenErr := uipersist.SettingsReadback(uipersist.StoreWriter{Store: m.services.store})
	for _, value := range values {
		raw, err := m.services.store.Setting(value.Key)
		if err != nil || raw != "" {
			t.Errorf("%s=%q err %v, want unwritten", value.Key, raw, err)
		}
	}
	if hiddenErr != nil || hidden != "" {
		t.Errorf("%s=%q err %v, want unwritten", uisettings.HiddenToolsSetting, hidden, hiddenErr)
	}
}

// settingsScriptWriter scripts failing writes and reads so the partial
// outcome can be proven deterministically.
type settingsScriptWriter struct {
	failOn    string
	failGetOn string
	values    map[string]string
}

func (w *settingsScriptWriter) SetSetting(key, value string) error {
	if w.failOn == key {
		return errors.New("scripted write failure")
	}
	w.values[key] = value
	return nil
}

func (w *settingsScriptWriter) SetProactiveCoordination(proactive bool) error {
	if w.failOn == "coordination" {
		return errors.New("scripted write failure")
	}
	w.values["coordination"] = "proactive"
	return nil
}

func (w *settingsScriptWriter) Setting(key string) (string, error) {
	if w.failGetOn == key {
		return "", errors.New("scripted read failure")
	}
	return w.values[key], nil
}

// Stage 1 red: saving the dialog must not write the store on the update
// path; the accepted effect writes the captured choices on completion.
func TestSettingsEnterDefersStoreWrites(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name

	_, cmd := m.handleSettingsKey(key("enter"))
	if m.settingsOpen() {
		t.Fatalf("enter should close the dialog, screen = %v", m.screenID())
	}
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("theme written on the update path: %q", got)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("stored theme=%q, want captured %q", got, staged)
	}
}

// Stage 1 red: leaving the CLI picker persists the captured visibility,
// not a store write inside the key handler.
func TestCLIPickerEscDefersHiddenWrite(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 1
	name := m.settingsCard().State().CLINames[1]
	m.handleSettingsKey(key(" "))
	if !m.settingsCard().State().CLIHidden[name] {
		t.Fatal("space should hide the focused CLI")
	}
	_, cmd := m.handleSettingsKey(key("esc"))
	if m.settingsCard().State().CLIPicker {
		t.Fatal("esc should leave the picker")
	}
	raw, err := m.services.store.Setting(uisettings.HiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Fatalf("hidden_tools written on the update path: %q", raw)
	}
	m.applyCmd(t, cmd)
	raw, err = m.services.store.Setting(uisettings.HiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != name {
		t.Fatalf("stored hidden_tools=%q, want captured %q", raw, name)
	}
}

// Stage 1 red: a save accepted while one choice is staged must keep that
// captured value; later dialog state may not retarget it.
func TestSettingsSaveKeepsCapturedChoices(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	captured := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	_, cmd := m.handleSettingsKey(key("enter"))

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	m.cycleSetting(1)
	m.applyCmd(t, cmd)

	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != captured {
		t.Fatalf("stored theme=%q, want captured %q", got, captured)
	}
}

// A partial failure commits what it can in order, reports the durable
// partial outcome, and a live dialog is restored to exactly the committed
// values.
func TestSettingsSavePartialFailureIsDurableAndHonest(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settingsCard().State().Field = uisettings.FieldCoordination
	m.cycleSetting(1)
	if !m.settingsCard().State().Proactive {
		t.Fatal("coordination should be staged proactive")
	}

	writer := &settingsScriptWriter{failOn: uisettings.ThemeAutoSetting, values: map[string]string{uisettings.ThemeSetting: "nord"}}
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}
	rawResult, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("expected the scripted failure")
	}
	result := rawResult
	if result.Committed != 2 || result.Tot != len(request.Values) {
		t.Fatalf("committed=%d tot=%d, want 2 of %d", result.Committed, result.Tot, len(request.Values))
	}
	if got := writer.values[uisettings.ThemeSetting]; got != staged {
		t.Fatalf("committed theme=%q, want %q", got, staged)
	}
	if _, ok := writer.values[uisettings.ThemeAutoSetting]; ok {
		t.Fatal("the failing key was written")
	}
	for _, later := range []string{uisettings.DiffLayoutSetting, uisettings.WorktreeSetting, "coordination"} {
		if _, ok := writer.values[later]; ok {
			t.Fatalf("write after the failure ran: %s", later)
		}
	}

	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, err))
	if m.errBar.text == "" {
		t.Fatal("partial failure should surface in the err bar")
	}
	if got := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name; got != staged {
		t.Fatalf("live dialog restored to %q, want committed %q", got, staged)
	}
	if m.settingsCard().State().Proactive {
		t.Fatal("live dialog kept the uncommitted proactive choice")
	}
}

// A failed committed-value read must not be presented as the committed
// value: that key keeps its pre-save state and the failure is reported.
func TestSettingsSaveReadFailureDoesNotFabricateRestoration(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	if !m.settingsCard().State().ComfortableRows {
		t.Fatal("density should stage comfortable")
	}

	writer := &settingsScriptWriter{failOn: uisettings.ThemeAutoSetting, failGetOn: uisettings.ListDensitySetting, values: map[string]string{}}
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}
	rawResult, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("expected the scripted write failure")
	}
	result := rawResult
	var readErr error
	for _, value := range result.Restored {
		if value.Key == uisettings.ListDensitySetting {
			readErr = value.Err
		}
	}
	if readErr == nil {
		t.Fatal("the failing read must carry its error in the result")
	}

	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, err))
	if !m.settingsCard().State().ComfortableRows {
		t.Fatal("a failed read was fabricated into the committed value")
	}
	if !strings.Contains(m.errBar.text, "reads failed") {
		t.Fatalf("read failures must be reported, err=%q", m.errBar.text)
	}
}

// Partial failure reconciles the live-session prefs as well as the dialog,
// so the runtime shows what persisted.
func TestSettingsSavePartialFailureReconcilesPrefs(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	m.applySettingsPrefs(m.settingsCard().State()) // the dispatch-time optimistic mirror
	if !m.prefs.comfortableRows {
		t.Fatal("prefs should stage comfortable before persistence")
	}

	writer := &settingsScriptWriter{failOn: uisettings.ListDensitySetting, values: map[string]string{}}
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}
	rawResult, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("expected the scripted write failure")
	}
	result := rawResult
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, err))
	if m.prefs.comfortableRows {
		t.Fatal("prefs kept the uncommitted density after a partial save")
	}
	if m.settingsCard().State().ComfortableRows {
		t.Fatal("dialog kept the uncommitted density after a partial save")
	}
}

// A stale completion must not touch the newer dialog's staged state, but
// it does reconcile the runtime prefs to the committed read-back.
func TestSettingsSaveStaleCompletionReconcilesPrefsNotDialog(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}

	m.settings.Gen++
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1) // the newer dialog toggles back to compact
	m.applySettingsPrefs(m.settingsCard().State())
	if m.prefs.comfortableRows {
		t.Fatal("the newer save's preview should be compact")
	}

	result := uipersist.SaveResult{Generation: request.Generation, Restored: []uisettings.Readback{
		{Key: uisettings.ListDensitySetting, Value: "comfortable"},
	}}
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, errors.New("partial")))
	if !m.prefs.comfortableRows {
		t.Fatal("stale completion must reconcile the runtime prefs to committed")
	}
	if m.settingsCard().State().ComfortableRows {
		t.Fatal("stale completion overwrote the newer dialog's staged state")
	}
	if m.errBar.text == "" {
		t.Fatal("the durable outcome should still be reported")
	}
}

// End to end: a full save fails on a locked store; while it is in flight
// the same dialog is reopened and a hidden-only save is queued behind it.
// The stale failure completion must reconcile the runtime prefs to the
// committed state without overwriting the newer dialog, and a single
// generation must not hide the earlier failure's applied preference.
func TestFailedFullSaveReconcilesPrefsPastStaleGeneration(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	locked, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locked.Close() })
	m.services.store = locked

	locker, err := sql.Open("sqlite", filepath.Join(dir, "state.db")+"?_pragma=busy_timeout(100)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locker.Close() })

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	m.applySettingsPrefs(m.settingsCard().State()) // the dispatch-time live preview
	if !m.prefs.comfortableRows {
		t.Fatal("dispatch must preview the staged density")
	}
	m.settings.Gen++
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}
	m.enqueueEffect(request, 0, false)
	command := m.nextEffectCmd()

	// The worker's writes now hit a locked database; its reads keep
	// working, so the committed read-back is authoritative.
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	completed := make(chan uieffects.Completed, 1)
	go func() { completed <- command().(uieffects.Completed) }()

	// While the failed save is in flight, the same dialog reopens and a
	// hidden-only save is queued behind it.
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldMouse
	m.cycleSetting(1)
	m.captureHiddenSave(m.settingsCard().State())

	msg := <-completed
	if msg.Err == nil {
		t.Fatal("expected the full save to fail on the locked store")
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
	if m.prefs.comfortableRows {
		t.Fatal("stale completion left the failed save's previewed density applied")
	}
	if !m.settingsCard().State().MouseDisabled {
		t.Fatal("stale completion overwrote the newer dialog's staged choice")
	}
	if m.errBar.text == "" {
		t.Fatal("the failed save must surface in the err bar")
	}
}

// An empty committed hidden state clears the dialog's map; only a failed
// hidden read leaves the old map in place.
func TestSettingsSaveEmptyHiddenClearsDialog(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().CLIHidden = map[string]bool{"claude": true}

	writer := &settingsScriptWriter{failOn: uisettings.HiddenToolsSetting, values: map[string]string{}}
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Hidden: []string{}, Generation: m.settings.Gen}
	rawResult, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("expected the hidden write to fail")
	}
	result := rawResult
	if result.HiddenErr != nil {
		t.Fatalf("hidden read failed: %v", result.HiddenErr)
	}
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, err))
	if len(m.settingsCard().State().CLIHidden) != 0 {
		t.Fatalf("committed empty hidden state did not clear the dialog: %v", m.settingsCard().State().CLIHidden)
	}

	// A failed hidden read keeps the old map.
	writer.failGetOn = uisettings.HiddenToolsSetting
	request2 := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Hidden: []string{}, Generation: m.settings.Gen}
	rawResult2, err2 := uipersist.RunSave(request2, writer)
	result2 := rawResult2
	if err2 == nil || result2.HiddenErr == nil {
		t.Fatal("expected a failed hidden read")
	}
	m.settingsCard().State().CLIHidden = map[string]bool{"codex": true}
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request2, result2, err2))
	if !m.settingsCard().State().CLIHidden["codex"] {
		t.Fatal("failed hidden read must keep the dialog's map")
	}
}

// A completion whose generation is older than the latest accepted save
// only reports; it never overwrites the newer dialog's state.
func TestSettingsSaveCompletionRefusesStaleGeneration(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}

	m.settings.Gen++
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	newer := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name

	result := uipersist.SaveResult{Generation: request.Generation, Restored: []uisettings.Readback{{Key: uisettings.ThemeSetting, Value: "nord"}}}
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, errors.New("partial")))
	if uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name != newer {
		t.Fatal("stale completion overwrote the newer dialog")
	}
	if m.errBar.text == "" {
		t.Fatal("the durable outcome should still be reported")
	}
}

// While the store is locked, the window keeps answering; the accepted
// save lands once the writer is free.
func TestSettingsSaveDoesNotBlockWindowUpdate(t *testing.T) {
	m := buildModel(t)
	dbPath := filepath.Join(t.TempDir(), "locked.db")
	locked, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locked.Close() })
	m.services.store = locked

	locker, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(100)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locker.Close() })
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	_, command := m.handleSettingsKey(key("enter"))

	completed := make(chan tea.Msg, 1)
	go func() { completed <- command() }()
	time.Sleep(300 * time.Millisecond) // the worker is now parked on the lock
	updated := make(chan struct{})
	go func() { m.Update(tea.WindowSizeMsg{Width: 150, Height: 45}); close(updated) }()
	select {
	case <-updated:
	case <-time.After(time.Second):
		locker.Exec("COMMIT")
		<-completed
		t.Fatal("window update waited on the blocked settings writer")
	}
	if _, err := locker.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	msg := <-completed
	m.applyTestMsg(t, msg)
	m.drainEffects(t)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("stored theme=%q, want %q", got, staged)
	}
}

// An accepted settings save is not discarded by quit; it drains FIFO.
func TestEffectQueueDrainsSettingsSaveOnQuit(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settings.Gen++
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}
	m.enqueueEffect(request, 0, false)

	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded the accepted save")
	}
	m.drainEffects(t)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("FIFO final theme=%s, want %s", got, staged)
	}
}

// Two hidden saves accepted back to back each commit; the older
// completion must not undo the newer one's state.
func TestCLIPickerSavesFenceEachOther(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 0
	first := m.settingsCard().State().CLINames[0]
	m.handleSettingsKey(key(" "))
	_, cmd1 := m.handleSettingsKey(key("esc"))
	if cmd1 == nil {
		t.Fatal("first save accepted no command")
	}
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 1
	second := m.settingsCard().State().CLINames[1]
	m.handleSettingsKey(key(" "))
	m.handleSettingsKey(key("esc")) // enqueues the second save; its command dispatches after the first

	m.applyTestMsg(t, cmd1())
	m.drainEffects(t)
	raw, err := m.services.store.Setting(uisettings.HiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != first+","+second {
		t.Fatalf("stored hidden_tools=%q, want %q", raw, first+","+second)
	}
}

// Duplicate commands and completions write once.
func TestSettingsSaveDuplicateCompletionIsIdempotent(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settings.Gen++
	m.enqueueEffect(uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}, 0, false)
	command := m.nextEffectCmd()
	msg := command()
	command()
	m.applyTestMsg(t, msg)
	m.applyTestMsg(t, msg)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("stored theme=%q, want %q", got, staged)
	}
}

// The request keeps its own copies of the mutable dialog slices.
func TestSettingsRequestCopiesMutableValues(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().CLIHidden = map[string]bool{"claude": true}
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Hidden: m.settingsCard().State().HiddenList(), Generation: 1}
	m.enqueueEffect(request, 0, false)
	request.Values[0].Value = "changed"
	request.Hidden[0] = "changed"
	stored := m.effects.Main.Pending[0].Request.(uipersist.SaveRequest)
	if stored.Values[0].Value == "changed" || stored.Hidden[0] == "changed" {
		t.Fatal("request retained mutable dialog aliases")
	}
}

func TestLaterSuccessfulSettingsSaveReappliesCommittedPrefs(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	request := uipersist.SaveRequest{Values: []uisettings.Value{{Key: uisettings.ListDensitySetting, Value: "comfortable"}}}
	m.prefs.comfortableRows = false
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, uipersist.SaveResult{}, nil))
	if !m.prefs.comfortableRows {
		t.Fatal("successful queued save did not reconcile its committed preferences")
	}
}

func TestFailedSettingsSavePreservesUnsetDefaults(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.prefs.focusOnEnter || !m.prefs.arrowStep {
		t.Fatal("fixture does not start with enabled defaults")
	}
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	_, _ = m.closeSettings()
	job := m.effects.Main.Active
	request := job.Request.(uipersist.SaveRequest)
	writer := &settingsScriptWriter{failOn: uisettings.ListDensitySetting, values: map[string]string{}}
	result, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("expected failed save")
	}
	m.applyTestMsg(t, uipersist.ApplySave(settingsSaveRoot{m}, request, result, err))
	if !m.prefs.focusOnEnter || !m.prefs.arrowStep {
		t.Fatalf("failed density save changed unrelated defaults, focus=%v arrows=%v", m.prefs.focusOnEnter, m.prefs.arrowStep)
	}
}

// cycleSetting steps the focused row as ← or → would.
func (m *Model) cycleSetting(step int) tea.Cmd {
	return m.runSettingsResult(m.settingsCard().State(), m.settingsCard().State().Cycle(step))
}

// openSettingsCLIPicker opens the CLI picker from its row.
func (m *Model) openSettingsCLIPicker(t *testing.T) {
	t.Helper()
	m.settingsCard().State().Field = uisettings.FieldCLIs
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().CLIPicker {
		t.Fatal("the CLIs row did not open the picker")
	}
}

// storedFlag reads a two-way row from the store, as the startup mirror does.
func storedFlag(t *testing.T, m *Model, key string) bool {
	t.Helper()
	value, err := m.services.store.Setting(key)
	if err != nil {
		t.Fatal(err)
	}
	return uisettings.FlagOn(key, value)
}
