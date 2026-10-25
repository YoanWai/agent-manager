package ui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
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
	x, y := paintedCell(t, m, "enter/esc")
	m, cmd = leftClickCmd(m, x, y)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	m.drainEffects(t)
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
			m.cycleThemeSettled(t)

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
	m.cycleThemeSettled(t)
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
	m.cycleThemeSettled(t)
	captured := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	_, cmd := m.handleSettingsKey(key("enter"))

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleThemeSettled(t)
	m.cycleThemeSettled(t)
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
	m.cycleThemeSettled(t)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settingsCard().State().Field = uisettings.FieldCoordination
	m.cycleThemeSettled(t)
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

	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, err))
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
	m.cycleThemeSettled(t)
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

	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, err))
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
	m.cycleThemeSettled(t)
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
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, err))
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
	m.cycleThemeSettled(t)
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
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, errors.New("partial")))
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
	m.cycleThemeSettled(t)
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
	m.cycleThemeSettled(t)
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
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, err))
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
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request2, result2, err2))
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
	m.cycleThemeSettled(t)
	request := uipersist.SaveRequest{Values: m.settingsCard().State().Values(), Generation: m.settings.Gen}

	m.settings.Gen++
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleThemeSettled(t)
	newer := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name

	result := uipersist.SaveResult{Generation: request.Generation, Restored: []uisettings.Readback{{Key: uisettings.ThemeSetting, Value: "nord"}}}
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, errors.New("partial")))
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
	m.cycleThemeSettled(t)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	_, command := m.handleSettingsKey(key("enter"))
	// The staged theme's pane push runs first on the lane; the save is next.
	if active := m.effects.Main.Active; command == nil && active != nil {
		_, _ = m.Update(active.Command())
		if next := m.effects.Main.Active; next != nil {
			command = next.Command
		}
	}
	if command == nil {
		t.Fatal("enter queued no save")
	}

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
	m.cycleThemeSettled(t)
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
	m.cycleThemeSettled(t)
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
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, uipersist.SaveResult{}, nil))
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
	m.cycleThemeSettled(t)
	_, _ = m.closeSettings()
	job := m.effects.Main.Active
	request := job.Request.(uipersist.SaveRequest)
	writer := &settingsScriptWriter{failOn: uisettings.ListDensitySetting, values: map[string]string{}}
	result, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("expected failed save")
	}
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, err))
	if !m.prefs.focusOnEnter || !m.prefs.arrowStep {
		t.Fatalf("failed density save changed unrelated defaults, focus=%v arrows=%v", m.prefs.focusOnEnter, m.prefs.arrowStep)
	}
}

// cycleSetting steps the focused row as ← or → would.
func (m *Model) cycleSetting(step int) tea.Cmd {
	return m.runSettingsResult(m.settingsCard().State(), m.settingsCard().State().Cycle(step))
}

// cycleThemeSettled steps the focused row and finishes the pane push a theme
// change starts, so the lane is free for the save a test queues next.
func (m *Model) cycleThemeSettled(t *testing.T) {
	t.Helper()
	if command := m.cycleSetting(1); command != nil {
		m.applyTestMsg(t, command())
	}
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

func openSavedPromptsPanel(t *testing.T, m *Model) {
	t.Helper()
	m.runBatch(t, m.openSettings())
	m.settingsCard().State().Field = uisettings.FieldPrompts
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter}))
	if !m.settingsCard().State().Prompts.Open() {
		t.Fatalf("enter on the saved prompts row did not open them: %q", m.errBar.text)
	}
}

func pressSettings(m *Model, msg tea.KeyMsg) tea.Cmd {
	_, cmd := m.handleSettingsKey(msg)
	return cmd
}

func pasteSettings(m *Model, text string) tea.Cmd {
	return pressSettings(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
}

func savePrompt(t *testing.T, m *Model, name, text string) {
	t.Helper()
	if err := m.services.store.SaveSessionPreset("", store.SessionPreset{Name: name, Instructions: text}); err != nil {
		t.Fatal(err)
	}
}

func storedPrompts(t *testing.T, m *Model) []store.SessionPreset {
	t.Helper()
	rows, err := m.services.store.SessionPresets()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// Settings lists saved prompts as a row of its own.
func TestSettingsShowsTheSavedPromptsRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.runBatch(t, m.openSettings())
	if !strings.Contains(ansi.Strip(m.viewSettings()), "saved prompts") {
		t.Fatal("Settings has no saved prompts row")
	}
}

// A save, an edit, a discarded edit and a delete each reach the store on
// the effect lane, never inside Update.
func TestSavedPromptsPanelWritesThroughTheEffectLane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	openSavedPromptsPanel(t, m)
	pressSettings(m, runeKey("n"))
	pasteSettings(m, "  Role  ")
	pressSettings(m, tea.KeyMsg{Type: tea.KeyTab})
	pasteSettings(m, " \n literal\n\n")
	save := pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if save == nil || len(storedPrompts(t, m)) != 0 {
		t.Fatal("ctrl+s wrote inside Update")
	}
	if again := pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}); again != nil {
		t.Fatal("a second ctrl+s queued a second save")
	}
	m.runBatch(t, save)
	rows := storedPrompts(t, m)
	if len(rows) != 1 || rows[0].Name != "Role" || rows[0].Instructions != " \n literal\n\n" {
		t.Fatalf("saved %+v", rows)
	}

	pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	pasteSettings(m, "Renamed")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
	if rows = storedPrompts(t, m); len(rows) != 1 || rows[0].Name != "Renamed" || rows[0].Instructions != " \n literal\n\n" {
		t.Fatalf("rename saved %+v", rows)
	}

	pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressSettings(m, tea.KeyMsg{Type: tea.KeyTab})
	pasteSettings(m, "discard")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEsc}))
	if rows = storedPrompts(t, m); strings.Contains(rows[0].Instructions, "discard") {
		t.Fatal("esc saved the edit")
	}

	pressSettings(m, runeKey("d"))
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter}))
	if rows = storedPrompts(t, m); len(rows) != 0 {
		t.Fatalf("delete left %+v", rows)
	}
	if !m.settingsCard().State().Prompts.Open() || !m.settingsOpen() {
		t.Fatal("the panel closed under its own delete")
	}
}

// A name the catalog already holds is refused with the typing kept.
func TestSavedPromptsPanelKeepsTypingOnARefusedSave(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Taken", "existing")
	openSavedPromptsPanel(t, m)
	pressSettings(m, runeKey("n"))
	pasteSettings(m, "Taken")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
	if m.errBar.text == "" || !strings.Contains(ansi.Strip(m.viewSettings()), "Taken") {
		t.Fatalf("refused save: err %q", m.errBar.text)
	}
	pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	pasteSettings(m, "Fresh")
	pressSettings(m, tea.KeyMsg{Type: tea.KeyTab})
	pasteSettings(m, "text")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyCtrlS}))
	if rows := storedPrompts(t, m); len(rows) != 2 || m.errBar.text != "" {
		t.Fatalf("retry saved %+v, err %q", rows, m.errBar.text)
	}
}

// A read that answers after the panel was closed and opened again leaves
// the newer list alone.
func TestSavedPromptsPanelIgnoresALateRead(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.runBatch(t, m.openSettings())
	m.settingsCard().State().Field = uisettings.FieldPrompts
	late := pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter})()
	pressSettings(m, tea.KeyMsg{Type: tea.KeyEsc})
	savePrompt(t, m, "New", "text")
	m.runBatch(t, pressSettings(m, tea.KeyMsg{Type: tea.KeyEnter}))
	m.Update(late)
	if body := ansi.Strip(m.viewSettings()); !strings.Contains(body, "New") || strings.Contains(body, "No saved prompts") {
		t.Fatalf("a late read replaced the list:\n%s", body)
	}
}

// A click on the row opens the panel, the wheel walks a long list, and
// everything stays inside a small terminal.
func TestSavedPromptsPanelByMouseInASmallTerminal(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 40, 22
	for i := range 30 {
		savePrompt(t, m, fmt.Sprintf("Role %02d", i), "text")
	}
	m.runBatch(t, m.openSettings())
	for range uisettings.FieldPrompts {
		m = wheelOnCard(m, tea.MouseButtonWheelDown)
	}
	frame := m.viewSettings()
	if m.settingsCard().State().Field != uisettings.FieldPrompts || len(strings.Split(frame, "\n")) > m.layout.height {
		t.Fatalf("the wheel stopped on field %d", m.settingsCard().State().Field)
	}
	m, cmd := clickBody(t, m, "saved prompts")
	if m.runBatch(t, cmd); !m.settingsCard().State().Prompts.Open() {
		t.Fatal("a click on the saved prompts row did not open them")
	}
	for range 29 {
		m = wheelOnCard(m, tea.MouseButtonWheelDown)
	}
	frame = m.viewSettings()
	if !strings.Contains(frame, "Role 29") {
		t.Fatal("the wheel did not bring the last prompt into view")
	}
	lines := strings.Split(frame, "\n")
	if len(lines) > m.layout.height {
		t.Fatalf("frame has %d rows for a %d row terminal", len(lines), m.layout.height)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.layout.width {
			t.Fatalf("frame line wider than the terminal: %q", line)
		}
	}
	pressSettings(m, runeKey("e"))
	if frame = m.viewSettings(); len(strings.Split(frame, "\n")) > m.layout.height || !strings.Contains(frame, "ctrl+s") {
		t.Fatal("the editor does not fit the terminal")
	}
}

// A click on the blank line under the title opens nothing while the saved
// prompts row is scrolled out of view above the docs row.
func TestSettingsClickAboveTheBodyLeavesSavedPromptsClosed(t *testing.T) {
	m := buildModel(t)
	openBrowser = func(url string) error { t.Fatalf("opened %s", url); return nil }
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })
	m.runBatch(t, m.openSettings())
	d := m.settingsCard().State()
	d.Field = uisettings.FieldCount - 1
	hidden := func() bool { return !paints(m, "saved prompts") && paints(m, "docs") }
	m.layout.width, m.layout.height = 40, 30
	for m.layout.height > 8 && !hidden() {
		m.layout.height--
	}
	if !hidden() {
		t.Fatal("no height hides saved prompts and shows docs")
	}
	_, cmd := m.handleMouse(tea.MouseMsg{X: m.layout.card.bodyLeft, Y: m.layout.card.bodyTop - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.runBatch(t, cmd)
	if d.Prompts.Open() {
		t.Fatal("a click above the body opened saved prompts")
	}
}

func TestSymbolsExplicitChoiceAndPendingReopen(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	_, save := m.closeSettings()
	m.applyCmd(t, save)
	if raw, _ := m.services.store.Setting(uisettings.GlyphModeSetting); raw != "" {
		t.Fatal("untouched settings pinned symbols default")
	}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		m.applyCmd(t, m.openSettings())
		m.settingsCard().State().Field = uisettings.FieldGlyphs
		m.handleSettingsKey(key("right"))
		if m.prefs.glyphMode != mode {
			t.Fatal("symbols did not preview")
		}
		_, save = m.closeSettings()
		m.openSettings()
		if m.settingsCard().State().GlyphMode != mode {
			t.Fatal("reopen lost pending symbols")
		}
		_, unchanged := m.closeSettings()
		m.applyCmd(t, save)
		m.applyCmd(t, unchanged)
		m.drainEffects(t)
		if got := reloadModel(t, m).prefs.glyphMode; got != mode {
			t.Fatalf("saved symbols %q want %q", got, mode)
		}
	}
	fresh := buildModel(t)
	fresh.applyCmd(t, fresh.openSettings())
	fresh.settingsCard().State().Field = uisettings.FieldGlyphs
	fresh.handleSettingsKey(key("right"))
	fresh.handleSettingsKey(key("left"))
	_, save = fresh.closeSettings()
	fresh.applyCmd(t, save)
	if raw, _ := fresh.services.store.Setting(uisettings.GlyphModeSetting); raw != "advanced" {
		t.Fatal("cycling back did not pin explicit default")
	}
}

func TestSymbolsMouseSaveUsesPaintedRows(t *testing.T) {
	for _, width := range []int{34, 80, 160} {
		m := buildModel(t)
		m.layout.width, m.layout.height = width, 24
		m.applyCmd(t, m.openSettings())
		preparedView(m)
		m = leftClick(m, m.layout.card.bodyLeft-1, m.layout.card.bodyTop)
		if m.prefs.glyphMode != "" {
			t.Fatal("padding changed symbols")
		}
		if m, _ = clickBody(t, m, "symbols"); m.prefs.glyphMode != presentation.GlyphModeBasic {
			t.Fatal("symbols click missed")
		}
		if !paints(m, "Compatible") {
			t.Fatal("symbols label did not update")
		}
		x, y := paintedCell(t, m, "enter/esc")
		m, save := leftClickCmd(m, x, y)
		m.applyCmd(t, save)
		if raw, _ := m.services.store.Setting(uisettings.GlyphModeSetting); raw != "basic" || m.settingsOpen() {
			t.Fatal("mouse save did not persist and close")
		}
	}
}

func TestSymbolsRejectInvalidAndStaleReads(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	stale := uipersist.LoadedMsg{Request: uipersist.LoadRequest{Target: uipersist.DialogTarget, Generation: m.settings.Gen}, Result: uipersist.LoadResult{Values: []uisettings.Readback{{Key: uisettings.GlyphModeSetting, Value: "advanced"}}}}
	m.settingsCard().State().Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	m.handleSettingsLoaded(stale)
	if m.prefs.glyphMode != presentation.GlyphModeBasic {
		t.Fatal("load overwrote unsaved symbols")
	}
	_, save := m.closeSettings()
	m.applyCmd(t, save)
	if err := m.services.store.SetSetting(uisettings.GlyphModeSetting, "unknown"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.openSettings())
	if m.prefs.glyphMode != presentation.GlyphModeBasic || !strings.Contains(m.errBar.text, "unknown symbols setting") {
		t.Fatal("invalid symbols were not rejected")
	}
}

func TestSymbolsSaveFailureRestoresAndCanRetry(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	m.settingsCard().State().Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	request := uipersist.SaveRequest{Values: m.settingsCard().State().ChangedValues(), Generation: m.settings.Gen}
	writer := &settingsScriptWriter{failOn: uisettings.GlyphModeSetting, values: map[string]string{}}
	result, err := uipersist.RunSave(request, writer)
	if err == nil {
		t.Fatal("fixture write must fail")
	}
	m.applyTestMsg(t, m.settings.ApplySave(settingsSaveHost{m}, request, result, err))
	if m.prefs.glyphMode != "" || m.errBar.text == "" {
		t.Fatal("failed symbols save did not restore default and report failure")
	}
	m.settingsCard().State().Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	_, save := m.closeSettings()
	m.applyCmd(t, save)
	if raw, _ := m.services.store.Setting(uisettings.GlyphModeSetting); raw != "basic" {
		t.Fatal("retry did not save")
	}
}

func TestSettingsUpdateDoesNotStartAfterSymbolsSaveFails(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	m.release.Latest = "v0.3.0"
	m.settingsCard().State().Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	m.settingsCard().State().Field = uisettings.FieldUpdate
	m.handleSettingsKey(key("enter"))
	job := m.effects.Main.Active
	request := job.Request.(uipersist.SaveRequest)
	if !request.StagedUpdate {
		t.Fatal("update not captured")
	}
	if cmd := m.settings.ApplySave(settingsSaveHost{m}, request, uipersist.SaveResult{}, errors.New("disk full")); cmd != nil || m.release.Applying {
		t.Fatal("failed settings save still starts update")
	}
}

func TestOpenSettingsDefersBlockedStoreRead(t *testing.T) {
	m := buildModel(t)
	reader := &blockedSettingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
		values:  map[string]string{uisettings.WorktreeSetting: "on"},
	}
	opened := make(chan tea.Cmd, 1)
	go func() { opened <- m.openSettingsWithReader(reader) }()

	var cmd tea.Cmd
	select {
	case <-reader.started:
		close(reader.release)
		<-opened
		t.Fatal("opening settings read the store on the update path")
	case cmd = <-opened:
	}
	if cmd == nil {
		t.Fatal("opening settings did not return the deferred refresh")
	}
	select {
	case <-reader.started:
		t.Fatal("settings reader ran before its Bubble Tea command")
	default:
	}

	// Opening batches the store read with the editor probe; each runs as
	// its own command, so the blocked read holds only itself.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("opening settings should batch the store read with the editor probe")
	}
	completed := make(chan tea.Msg, len(batch))
	for _, each := range batch {
		go func() { completed <- each() }()
	}
	<-reader.started
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 151, Height: 45})
	m = updated.(*Model)
	if m.layout.width != 151 {
		t.Fatal("a blocked settings reader blocked an unrelated window update")
	}
	close(reader.release)
	for range batch {
		m.applyTestMsg(t, <-completed)
	}
	if !m.settingsCard().State().WorktreeDefault {
		t.Fatal("the accepted deferred refresh did not update the dialog")
	}
}

func TestOpenCLIPickerDoesNotReadStore(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}
	m.errBar.text = ""
	m.settingsCard().State().CLIHidden = nil
	m.openSettingsCLIPicker(t)
	if m.errBar.text != "" {
		t.Fatalf("opening the CLI picker read the closed store: %q", m.errBar.text)
	}
}

func TestSettingsOpenRefreshesExternalChanges(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(uisettings.HiddenToolsSetting, "claude"); err != nil {
		t.Fatal(err)
	}

	cmd := m.openSettings()
	if m.settingsCard().State().WorktreeDefault || m.settingsCard().State().CLIHidden["claude"] {
		t.Fatal("opening should paint the cached preferences before the refresh")
	}
	m.applyTestMsg(t, cmd())
	if !m.settingsCard().State().WorktreeDefault || !m.settingsCard().State().CLIHidden["claude"] {
		t.Fatalf("external settings were not refreshed: worktree=%t hidden=%v", m.settingsCard().State().WorktreeDefault, m.settingsCard().State().CLIHidden)
	}
}

func TestSettingsLoadRefusesReopenedDialog(t *testing.T) {
	m := buildModel(t)
	cmd := m.openSettings()
	firstGeneration := m.settings.Gen
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatal(err)
	}
	loaded := cmd()

	m.openSettings()
	if m.settings.Gen == firstGeneration {
		t.Fatal("reopening settings reused the old generation")
	}
	m.applyTestMsg(t, loaded)
	if m.settingsCard().State().WorktreeDefault {
		t.Fatal("the first dialog's load overwrote the reopened dialog")
	}
}

func TestSettingsLoadRefusesEditsAndSubpickers(t *testing.T) {
	t.Run("staged edit", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		m.settingsCard().State().Field = uisettings.FieldWorktree
		m.cycleThemeSettled(t)
		m.applyTestMsg(t, cmd())
		if !m.settingsCard().State().WorktreeDefault {
			t.Fatal("the async load overwrote a staged setting")
		}
	})

	t.Run("CLI picker", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		if err := m.services.store.SetSetting(uisettings.HiddenToolsSetting, "claude"); err != nil {
			t.Fatal(err)
		}
		m.openSettingsCLIPicker(t)
		m.applyTestMsg(t, cmd())
		if m.settingsCard().State().CLIHidden["claude"] {
			t.Fatal("the async load replaced an open CLI picker")
		}
	})

	t.Run("key picker", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		m.settingsCard().State().KeyPicker = true
		before := m.settingsCard().State().WorktreeDefault
		if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
			t.Fatal(err)
		}
		m.applyTestMsg(t, cmd())
		if m.settingsCard().State().WorktreeDefault != before {
			t.Fatal("the async load replaced an open key picker")
		}
	})
}

func TestSettingsReopenUsesOptimisticPendingSave(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldWorktree
	m.cycleThemeSettled(t)
	_, save := m.handleSettingsKey(key("enter"))
	if save == nil {
		t.Fatal("settings save was not queued")
	}

	// Only the editor PATH probe may run; a store load would race the save.
	if refresh := m.openSettings(); refresh != nil {
		if _, probe := refresh().(uisettings.EditorsProbe); !probe {
			t.Fatal("a dialog reopened over a pending save should use the optimistic cache")
		}
	}
	if !m.settingsCard().State().WorktreeDefault {
		t.Fatal("the reopened dialog lost the pending worktree choice")
	}
	m.applyCmd(t, save)
}

func specByKey(t *testing.T) map[string]store.SettingSpec {
	t.Helper()
	specs, err := SettingSpecs()
	if err != nil {
		t.Fatalf("SettingSpecs: %v", err)
	}
	byKey := map[string]store.SettingSpec{}
	for _, spec := range specs {
		if _, dup := byKey[spec.Key]; dup {
			t.Fatalf("SettingSpecs names %s twice", spec.Key)
		}
		byKey[spec.Key] = spec
	}
	return byKey
}

// The dialog spells the keys the store reads the same way.
func TestSettingKeysMatchTheStore(t *testing.T) {
	for got, want := range map[string]string{
		uisettings.DefaultToolSetting:  store.DefaultToolSetting,
		uisettings.CoordinationSetting: store.CoordinationSetting,
		uisettings.EditorSetting:       store.EditorSetting,
		uisettings.HiddenToolsSetting:  store.HiddenToolsSetting,
	} {
		if got != want {
			t.Errorf("settings key %q, store reads %q", got, want)
		}
	}
}

// Every key the modal writes is one the shell can set, with a value the
// shell would accept, so the two fronts never drift apart.
func TestSettingSpecsCoverEveryKeySettingsWrites(t *testing.T) {
	m := buildModel(t)
	before, err := m.services.store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.openSettings())
	m.openSettingsCLIPicker(t)
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	// A save writes only the rows that changed; with no baseline every row
	// counts as changed, as on a fresh profile's first save.
	m.settingsCard().State().Baseline = nil
	m.settingsCard().State().BaselineHidden = nil
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	// Only the save's own effects: a refresh would stamp the poller's
	// runtime rows, which are not settings.
	if cmd != nil {
		m.applyTestMsg(t, cmd())
	}
	m.drainEffects(t)
	after, err := m.services.store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	specs := specByKey(t)
	written := 0
	for key, value := range after {
		if _, old := before[key]; old {
			continue
		}
		written++
		spec, known := specs[key]
		if !known {
			t.Errorf("Settings writes %s, which SettingSpecs does not name", key)
			continue
		}
		if spec.Row == "" {
			t.Errorf("Settings writes %s, which SettingSpecs names with no row", key)
		}
		if err := spec.Check(value); err != nil {
			t.Errorf("Settings wrote %s=%q: %v", key, value, err)
		}
	}
	if written == 0 {
		t.Fatal("closing Settings wrote nothing, so the test checked nothing")
	}
	for key, spec := range specs {
		if _, present := after[key]; !present && spec.Row != "" {
			t.Errorf("SettingSpecs names %s (%s), which closing Settings never writes", key, spec.Row)
		}
	}
}

func TestSettingSpecsNameEveryThemeAndAgentCLI(t *testing.T) {
	specs := specByKey(t)
	var names []string
	for _, theme := range uitheme.Palettes {
		names = append(names, theme.Name)
	}
	if !slices.Equal(specs[uisettings.ThemeSetting].Values, names) {
		t.Fatalf("theme takes %v, want the shipped palettes %v", specs[uisettings.ThemeSetting].Values, names)
	}
	if !slices.Contains(specs[store.DefaultToolSetting].Values, "claude") || slices.Contains(specs[store.DefaultToolSetting].Values, "terminal") {
		t.Fatalf("default_tool takes %v, want the agent CLIs without the shell", specs[store.DefaultToolSetting].Values)
	}
	if !specs[store.HiddenToolsSetting].List {
		t.Fatal("hidden_tools should take a comma list")
	}
}

func TestSettingSpecsDefaultsAreValues(t *testing.T) {
	for key, spec := range specByKey(t) {
		if spec.Default == "" {
			continue
		}
		if err := spec.Check(spec.Default); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}

// A value the shell changed while the manager was open shows in Settings
// and survives esc, instead of the modal writing back what the manager
// cached at startup.
func TestOpenSettingsReadsWhatTheShellChanged(t *testing.T) {
	m := buildModel(t)
	changed := map[string]string{
		uisettings.GlyphModeSetting:     "basic",
		uisettings.ListDensitySetting:   "comfortable",
		uisettings.SessionLayoutSetting: "full",
		uisettings.HideHeaderSetting:    "on",
		uisettings.HideStatsSetting:     "on",
		uisettings.MouseSetting:         "off",
		uisettings.ArrowStepSetting:     "off",
		uisettings.FocusKeySetting:      "attach",
		uisettings.BaseFetchSetting:     "off",
		uisettings.BackgroundSetting:    "terminal",
		uisettings.ThemeSetting:         "nord",
	}
	for key, value := range changed {
		if err := m.services.store.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetEditor("code -n"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.openSettings())
	if uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name != "nord" {
		t.Fatalf("Settings opened on %q, want the shell's nord", uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name)
	}
	if m.settingsCard().State().GlyphMode != presentation.GlyphModeBasic || m.prefs.glyphMode != presentation.GlyphModeBasic {
		t.Fatalf("symbols opened on %q (live %q), want basic", m.settingsCard().State().GlyphMode, m.prefs.glyphMode)
	}
	if uitheme.Current().Name != "nord" {
		t.Fatalf("the palette stayed %q after Settings read nord", uitheme.Current().Name)
	}
	if m.settingsCard().State().Editor.Line() != "code -n" {
		t.Fatalf("editor row opened on %q", m.settingsCard().State().Editor.Line())
	}
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	for key, want := range changed {
		if got, _ := m.services.store.Setting(key); got != want {
			t.Errorf("esc wrote %s=%q over the shell's %q", key, got, want)
		}
	}
	if p := m.prefs; !p.comfortableRows || !p.fullLayout || !p.hideHeader || !p.hideStats || !p.mouseDisabled || p.arrowStep || p.focusOnEnter || !p.baseFetchOff || !p.terminalBackground {
		t.Fatal("esc did not apply the shell's values to the running manager")
	}
}

func TestSettingsViewportKeepsFocusedFieldAndSaveVisible(t *testing.T) {
	m := buildModel(t)
	for _, size := range [][2]int{{34, 12}, {80, 24}, {160, 42}} {
		m.layout.width, m.layout.height = size[0], size[1]
		m.openSettings()
		for _, field := range []int{uisettings.FieldGlyphs, uisettings.FieldKeybindings, uisettings.FieldUpdate} {
			m.settingsCard().State().Field = field
			view := ansi.Strip(m.viewSettings())
			lines := strings.Split(view, "\n")
			if len(lines) > m.layout.height {
				t.Fatalf("Settings exceeds height %d: %d", m.layout.height, len(lines))
			}
			want := map[int]string{uisettings.FieldGlyphs: "symbols", uisettings.FieldKeybindings: "keybindings", uisettings.FieldUpdate: "version"}[field]
			for _, text := range []string{want, "save and close", "esc"} {
				if !strings.Contains(view, text) {
					t.Fatalf("Settings %dx%d lost %q:\n%s", m.layout.width, m.layout.height, text, view)
				}
			}
			if strings.Contains(view, "[ Save and close ]") {
				t.Fatalf("Settings %dx%d kept a save button in the rows:\n%s", m.layout.width, m.layout.height, view)
			}
			if field != uisettings.FieldGlyphs && !strings.Contains(view, "symbols") {
				m = leftClick(m, m.layout.card.bodyLeft, m.layout.card.bodyTop-1)
				if m.prefs.glyphMode != "" {
					t.Fatal("hidden symbols row became a click target")
				}
			}
		}
	}
}

// Save and close stays in the main dialog's footer. The key picker scrolls
// its own list and leaves with esc.
func TestKeyPickerCardCarriesNoSaveButton(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	m.handleSettingsKey(key("enter"))
	if !m.settingsCard().State().KeyPicker {
		t.Fatal("test setup: enter on keybindings opens the picker")
	}
	view := ansi.Strip(m.viewSettings())
	if strings.Contains(view, "save and close") {
		t.Fatalf("the key picker painted the main dialog's save hint:\n%s", view)
	}
	if !strings.Contains(view, "detach") {
		t.Fatalf("the key picker lost its first row:\n%s", view)
	}
}

// A flex card is wider than the default, so its hint can sit on fewer rows;
// the room left for the body is counted at the card's own width.
func TestFlexChromeRowsMatchThePaintedCard(t *testing.T) {
	m := buildModel(t)
	hint := [][2]string{{"↑↓", "move"}, {"↵", "set a key"}, {"a", "add one"}, {"d", "off"}, {"r", "defaults"}, {"esc", "back"}}
	body := strings.Repeat("a row wide enough to stretch the card past its default width\n", 5) + "last"
	for _, width := range []int{40, 64, 90, 160} {
		m.layout.width, m.layout.height = width, 60
		m.CardSized(m.flexCardWidth("title", body, hint), "title", body, hint)
		painted := m.layout.card.bottom - m.layout.card.top - (strings.Count(body, "\n") + 1)
		if got := m.flexChromeRows(hint); got != painted {
			t.Fatalf("width %d: flexChromeRows = %d, the card painted %d", width, got, painted)
		}
	}
}

// The key picker spends every row the terminal has on its list, short of
// the one a table heading can push past the window.
func TestKeyPickerFillsTheTerminal(t *testing.T) {
	m := buildModel(t)
	m.layout.width = 160
	for height := 16; height <= 40; height++ {
		m.layout.height = height
		m.openSettings()
		m.settingsCard().State().Field = uisettings.FieldKeybindings
		m.handleSettingsKey(key("enter"))
		m.viewSettings()
		if rows := m.layout.card.bottom - m.layout.card.top; rows > height || rows < height-1 {
			t.Fatalf("height %d: the key picker card is %d rows tall", height, rows)
		}
	}
}

func TestCompatibleCardsKeepTitlesAndClickGeometry(t *testing.T) {
	m := buildModel(t)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{34, 80, 120} {
			if err := m.services.store.SetSetting(uisettings.GlyphModeSetting, string(mode)); err != nil {
				t.Fatal(err)
			}
			m.layout.width, m.layout.height = width, 24
			m.applyCmd(t, m.openSettings())
			m.settingsCard().State().Field = uisettings.FieldGlyphs
			got := ansi.Strip(m.viewSettings())
			title, cursor := "# Settings", "> symbols"
			if mode == presentation.GlyphModeAdvanced {
				title, cursor = "⚙ Settings", "❯ symbols"
			}
			if !strings.Contains(got, title) || (width >= 36 && !strings.Contains(got, cursor)) {
				t.Fatalf("%s/%d Settings missing title or cursor:\n%s", mode, width, got)
			}
			m, _ = clickBody(t, m, "symbols")
			if m.prefs.glyphMode.Effective() == mode {
				t.Fatal("symbols click missed its painted row")
			}
			m.applyCmd(t, m.openSettings())
		}
	}
}

func TestDefaultSplitLayout(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if !m.defaultSplitLayout() {
		t.Fatal("split should be the default layout")
	}
	if err := m.services.store.SetSetting(uisettings.DiffLayoutSetting, "unified"); err != nil {
		t.Fatal(err)
	}
	if m.defaultSplitLayout() {
		t.Fatal("stored unified choice should opt out of split")
	}
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
		m.cycleThemeSettled(t)
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

func settingsModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 50
	m.applyCmd(t, m.openSettings())
	return m
}

func TestClickOnASettingsRowMovesThenSteps(t *testing.T) {
	m := settingsModel(t)
	x, y := paintedCell(t, m, "background")
	m = leftClick(m, x, y)
	d := m.settingsCard().State()
	if d.Field != uisettings.FieldBackground || d.TerminalBackground {
		t.Fatalf("first click should only focus the row, field %d background %v", d.Field, d.TerminalBackground)
	}
	m = leftClick(m, x, y)
	if !m.settingsCard().State().TerminalBackground || !m.prefs.terminalBackground {
		t.Fatal("second click should step the row and preview it")
	}
	m = wheel(m, tea.MouseButtonWheelDown, x, y)
	if m.settingsCard().State().Field != uisettings.FieldBackground+1 {
		t.Fatalf("the wheel should move to the next field, field %d", m.settingsCard().State().Field)
	}
}

func TestClickOnTheCLIsRowManagesThem(t *testing.T) {
	m := settingsModel(t)
	x, y := paintedCell(t, m, "CLIs")
	m = leftClick(m, x, y)
	m = leftClick(m, x, y)
	if !m.settingsCard().State().CLIPicker {
		t.Fatal("two clicks on CLIs should open the picker")
	}
	names := m.settingsCard().State().CLINames
	if len(names) < 2 {
		t.Fatalf("test setup: the picker needs two CLIs, has %v", names)
	}
	x, y = paintedCell(t, m, "] "+names[1])
	if m = leftClick(m, x, y); !m.settingsCard().State().CLIHidden[names[1]] {
		t.Fatalf("one click on %s's checkbox should toggle it", names[1])
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.settingsCard().State().CLICursor != 2 {
		t.Fatalf("the wheel should move the picker cursor, at %d", m.settingsCard().State().CLICursor)
	}
}

func TestClickOnTheKeyPickerSetsAKey(t *testing.T) {
	m := settingsModel(t)
	x, y := paintedCell(t, m, "keybindings")
	m = leftClick(m, x, y)
	m = leftClick(m, x, y)
	if !m.settingsCard().State().KeyPicker {
		t.Fatal("two clicks on keybindings should open the picker")
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.settingsCard().State().KeyCursor != 1 {
		t.Fatalf("the wheel should move the picker cursor, at %d", m.settingsCard().State().KeyCursor)
	}
	x, y = paintedCell(t, m, "editor  ")
	m = leftClick(m, x, y)
	if row := m.settingsCard().State().PickedRow(); row.Action.Name != "editor" || m.settingsCard().State().KeyCapture {
		t.Fatalf("first click should move to editor, at %q capture %v", row.Action.Name, m.settingsCard().State().KeyCapture)
	}
	if m = leftClick(m, x, y); !m.settingsCard().State().KeyCapture {
		t.Fatal("a click on the row under the cursor should start capture")
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.settingsCard().State().PickedRow().Action.Name != "editor" {
		t.Fatal("the wheel must not move the row a capture is binding")
	}
}

// editorSettings opens Settings on the editor row with the probe already
// answered for a machine that has these editors.
func editorSettings(t *testing.T, m *Model, installed ...string) {
	t.Helper()
	captureEditor(t, installed...)
	m.applyCmd(t, m.openSettings())
	m.settingsCard().State().Field = uisettings.FieldEditor
}

func (m *Model) pressInSettings(t *testing.T, msg tea.KeyMsg) {
	t.Helper()
	updated, _ := m.handleKey(msg)
	*m = *updated.(*Model)
}

// The editors the shell names are offered beside the ones found on PATH,
// once each, so a terminal editor is a step away rather than a typed line.
func TestProbeOffersTheShellsEditors(t *testing.T) {
	captureEditor(t, "code")
	t.Setenv("VISUAL", "code")
	t.Setenv("EDITOR", " nvim ")
	probed := editorProbe(buildModel(t).captureEditorResolution())
	if got := strings.Join(probed.Found, ","); got != "code,nvim" {
		t.Fatalf("found = %q, want code once and nvim", got)
	}
	row := uisettings.NewEditorRow("nvim", &probed)
	if row.Custom() || row.Label() != "nvim" {
		t.Fatalf("a stored nvim should sit on its own entry, got %q custom %v", row.Label(), row.Custom())
	}
}

// The picked editor is stored, survives a restart, and is what the editor
// key opens, ahead of the one auto would have taken.
func TestSettingsEditorRowStoresThePickedEditor(t *testing.T) {
	m := buildModel(t)
	editorSettings(t, m, "code", "zed")
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "auto · code") {
		t.Fatalf("the row should name what auto opens:\n%s", view)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyRight})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyRight})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsOpen() {
		t.Fatalf("esc should save and leave, screen = %v", m.screenID())
	}
	m.drainEffects(t)
	if stored, err := m.services.store.Editor(); err != nil || stored != "zed" {
		t.Fatalf("stored editor = %q, %v", stored, err)
	}
	if got := reloadModel(t, m).captureEditorResolution().Line(); got != "zed" {
		t.Fatalf("a restarted manager opens %q, want zed", got)
	}
}

// Custom is the one entry that is typed. While the field is open every key
// is text, so a letter that moves the cursor elsewhere types instead.
func TestSettingsEditorCustomLineIsTyped(t *testing.T) {
	m := buildModel(t)
	editorSettings(t, m, "code")
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyLeft})
	if !m.settingsCard().State().Editor.Custom() {
		t.Fatalf("left from auto should land on custom, got %q", m.settingsCard().State().Editor.Label())
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().Editor.Typing() || !m.settingsOpen() {
		t.Fatal("enter on custom should open the field, not save")
	}
	m.pressInSettings(t, runeKey("junk"))
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsCard().State().Editor.Typing() || m.settingsCard().State().Editor.Line() != "" || !m.settingsOpen() {
		t.Fatalf("esc should drop what was typed and stay in Settings, line = %q", m.settingsCard().State().Editor.Line())
	}

	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInSettings(t, runeKey("code -n k"))
	if m.settingsCard().State().Field != uisettings.FieldEditor {
		t.Fatal("a typed k must not move the cursor off the row")
	}
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "code -n k") {
		t.Fatalf("the field should show what is typed:\n%s", view)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyBackspace})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyBackspace})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.settingsCard().State().Editor.Label(); got != "custom · code -n" {
		t.Fatalf("label = %q", got)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	if stored, err := m.services.store.Editor(); err != nil || stored != "code -n" {
		t.Fatalf("stored editor = %q, %v", stored, err)
	}
	if got := m.captureEditorResolution().Line(); got != "code -n" {
		t.Fatalf("resolved editor = %q", got)
	}
}

// Stepping back to auto clears the stored line, so the environment and
// PATH decide again.
func TestSettingsEditorRowReturnsToAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetEditor("code -n"); err != nil {
		t.Fatalf("SetEditor: %v", err)
	}
	m.services.editor = "code -n"
	editorSettings(t, m, "zed")
	if got := m.settingsCard().State().Editor.Label(); got != "custom · code -n" {
		t.Fatalf("label = %q", got)
	}
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyRight})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	if stored, err := m.services.store.Editor(); err != nil || stored != "" {
		t.Fatalf("stored editor = %q, %v", stored, err)
	}
	if got := m.captureEditorResolution().Line(); got != "zed" {
		t.Fatalf("resolved editor = %q, want the editor on PATH", got)
	}
}

// keyPickerModel opens the settings screen and steps into the key picker.
func keyPickerModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.services.keys = keybind.DefaultSession()
	m.services.tmux.SetSessionKeys(m.services.keys)
	t.Cleanup(func() {
		m.services.tmux.SetSessionKeys(keybind.DefaultSession())
		if err := m.services.tmux.EnsureBindings(); err != nil {
			t.Errorf("restore default bindings: %v", err)
		}
	})
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !m.settingsCard().State().KeyPicker {
		t.Fatalf("enter on the keys row should open the picker, err = %q", m.errBar.text)
	}
	return m
}

func (m *Model) pressInPicker(t *testing.T, msg tea.KeyMsg) tea.Cmd {
	t.Helper()
	updated, cmd := m.handleKey(msg)
	*m = *updated.(*Model)
	return cmd
}

func storedSessionKeys(t *testing.T, m *Model) keybind.Table {
	t.Helper()
	keys, err := m.services.store.SessionKeys()
	if err != nil {
		t.Fatalf("SessionKeys: %v", err)
	}
	return keys
}

// storedKeyRow is the raw row a scope's table is kept in, empty until the
// picker saves that table.
func storedKeyRow(t *testing.T, m *Model, scope string) string {
	t.Helper()
	row, err := m.services.store.Setting("keybindings." + scope)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	return row
}

// The picker binds the key that was pressed: leaving it writes the table to
// the store, puts it on the model, and rebinds the tmux server so a live
// session answers to the new key.
func TestKeyPickerBindsCapturedKeyAndSavesIt(t *testing.T) {
	m := keyPickerModel(t)
	// A live session, bound with the old keys before the change: psmux
	// keeps bindings per session server, so the rebind is checked there.
	live := newID()
	if err := m.services.tmux.Create(live, t.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("create live session: %v", err)
	}
	t.Cleanup(func() { _ = m.services.tmux.Kill(live) })
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().KeyCapture {
		t.Fatal("enter on an action should wait for a key")
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("detach = %q, want f9", got)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsCard().State().KeyPicker {
		t.Fatal("esc should leave the picker")
	}
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	if cmd == nil {
		t.Fatal("saving should enqueue the save work")
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("runtime detach changed before the save committed: %q", got)
	}
	if row := storedKeyRow(t, m, keybind.ScopeSession); row != "" {
		t.Fatalf("the store was written on the update path: %s", row)
	}
	// The completion's refresh command carries the tmux rebind; run it the
	// way the event loop would.
	updated, next := m.Update(cmd())
	*m = *updated.(*Model)
	m.applyTestMsg(t, next())
	m.drainEffects(t)
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("model detach = %q, want f9", got)
	}
	if got := storedSessionKeys(t, m).Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("stored detach = %q, want f9", got)
	}

	bound, err := sessionTmuxCmd(t, live, "list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	if !strings.Contains(string(bound), "F9") {
		t.Fatalf("the server should carry the new detach key:\n%s", bound)
	}
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[3] == "C-q" && strings.Contains(line, "detach-client") {
			t.Fatalf("the old detach key should be gone: %q", line)
		}
	}
}

// A key the manager cannot bind is refused in the picker with the reason,
// and nothing changes.
func TestKeyPickerRefusesAKeyTheAgentNeeds(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("o"))
	if !strings.Contains(m.errBar.text, "plain key, which reaches the agent") {
		t.Fatalf("err = %q, want the plain-key reason", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}
}

// One key serves one action, so binding review to the editor's key is
// refused rather than leaving two actions on it.
func TestKeyPickerRefusesAKeyAnotherActionOwns(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 1
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF3})
	if !strings.Contains(m.errBar.text, "bound to both") {
		t.Fatalf("err = %q, want the shared-key reason", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Review).Label(); got != "ctrl+r" {
		t.Fatalf("review should be untouched, got %q", got)
	}
}

// d hands an action's key to the agent, and refuses to do it for detach,
// which is the way back from a focused session.
func TestKeyPickerTurnsAnActionOffButKeepsAWayBack(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 2
	m.pressInPicker(t, runeKey("d"))
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Editor).Label(); got != "" {
		t.Fatalf("editor should be off, got %q", got)
	}

	m.settingsCard().State().KeyCursor = 0
	m.pressInPicker(t, runeKey("d"))
	if !strings.Contains(m.errBar.text, "detach needs at least one key") {
		t.Fatalf("err = %q, want the detach rule", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}

	m.settingsCard().State().KeyCursor = 2
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if got := storedSessionKeys(t, m).Binding(keybind.Editor).Label(); got != "" {
		t.Fatalf("the store should record the disabled action, got %q", got)
	}
}

// a adds a second key to an action rather than replacing what it answers to.
func TestKeyPickerAddsASecondKey(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 1
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Review).Label(); got != "ctrl+r / f9" {
		t.Fatalf("review = %q, want both keys", got)
	}
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if !strings.Contains(m.errBar.text, "already answers to") {
		t.Fatalf("err = %q, want the duplicate note", m.errBar.text)
	}
}

// esc during capture cancels it; the key that would have been bound is not.
func TestKeyPickerResetsEveryActionToItsDefaultAfterAsking(t *testing.T) {
	m := keyPickerModel(t)
	custom := sessionOf(t, []string{"ctrl+q", "f9"}, nil, []string{"f5"})
	customList := keybind.DefaultList().With(keybind.NewSession, bindingOf(t, "N"))
	m.services.keys, m.services.listKeys = custom, customList
	m.services.tmux.SetSessionKeys(custom)
	m.settingsCard().State().Tables[0], m.settingsCard().State().Tables[1] = custom, customList

	m.pressInPicker(t, runeKey("r"))
	if !m.settingsCard().State().KeyReset {
		t.Fatal("r should ask before resetting")
	}
	ask := ansi.Strip(m.viewSettings())
	for _, want := range []string{"Reset every key", "detach: ctrl+q / f9 back to ctrl+q / ctrl+\\", "review: off back to ctrl+r", "editor: f5 back to f3", "new_session: N back to n"} {
		if !strings.Contains(ask, want) {
			t.Fatalf("the question should say %q:\n%s", want, ask)
		}
	}
	m.pressInPicker(t, runeKey("n"))
	if m.settingsCard().State().KeyReset || !m.settingsCard().State().Tables[0].Equal(custom) {
		t.Fatalf("n should keep the keys, got %s", m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label())
	}

	m.pressInPicker(t, runeKey("r"))
	m.pressInPicker(t, runeKey("y"))
	if m.settingsCard().State().KeyReset || !m.settingsCard().State().Tables[0].Equal(keybind.DefaultSession()) || !m.settingsCard().State().Tables[1].Equal(keybind.DefaultList()) {
		t.Fatalf("y should restore both tables, got %s and new_session %s", m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(), m.settingsCard().State().Tables[1].Binding(keybind.NewSession).Label())
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if !m.services.keys.Equal(keybind.DefaultSession()) || !m.services.listKeys.Equal(keybind.DefaultList()) {
		t.Fatalf("model keys after save = %s, new_session %s", m.services.keys.Binding(keybind.Detach).Label(), m.services.listKeys.Binding(keybind.NewSession).Label())
	}
	if !storedSessionKeys(t, m).Equal(keybind.DefaultSession()) {
		t.Fatalf("stored session keys = %s", storedKeyRow(t, m, keybind.ScopeSession))
	}
	storedList, err := m.services.store.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if !storedList.Equal(keybind.DefaultList()) {
		t.Fatalf("stored list keys = %s", storedKeyRow(t, m, keybind.ScopeList))
	}
}

func TestKeyPickerResetOnDefaultsAsksNothing(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, runeKey("r"))
	if m.settingsCard().State().KeyReset {
		t.Fatal("r on the defaults should not open the question")
	}
}

func TestKeyPickerEscapeCancelsCapture(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsCard().State().KeyCapture {
		t.Fatal("esc should end the capture")
	}
	if !m.settingsCard().State().KeyPicker {
		t.Fatal("cancelling a capture should stay in the picker")
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}
}

// Leaving the picker without a change writes nothing, so an action nobody
// moved keeps following the default a later release gives it.
func TestKeyPickerLeavesTheStoreAloneWithoutAChange(t *testing.T) {
	m := keyPickerModel(t)
	if cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("an unchanged table should not refresh the sessions")
	}
	m.drainEffects(t)
	for _, scope := range []string{keybind.ScopeSession, keybind.ScopeList} {
		if row := storedKeyRow(t, m, scope); row != "" {
			t.Fatalf("the %s table should not be stored: %s", scope, row)
		}
	}
}

func TestKeyPickerViewNamesTheKeysAndTheCapture(t *testing.T) {
	m := keyPickerModel(t)
	view := ansi.Strip(m.viewSettings())
	for _, want := range []string{"detach", `ctrl+q / ctrl+\`, "back to the manager", "every other key reaches the agent"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker view is missing %q:\n%s", want, view)
		}
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if capture := ansi.Strip(m.viewSettings()); !strings.Contains(capture, "press a key") {
		t.Fatalf("a waiting row should say so:\n%s", capture)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.settingsCard().State().KeyCursor = 2
	m.pressInPicker(t, runeKey("d"))
	if off := ansi.Strip(m.viewSettings()); !strings.Contains(off, "off, the agent gets it") {
		t.Fatalf("a disabled action should say where its key goes:\n%s", off)
	}
}

// The settings row names the keys in force, so the screen answers the
// question without opening the picker.
func TestSettingsRowCountsTheMovedKeys(t *testing.T) {
	m := buildModel(t)
	m.services.keys = keybind.DefaultSession()
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	view := ansi.Strip(m.viewSettings())
	if !strings.Contains(view, "keybindings") || !strings.Contains(view, "defaults") {
		t.Fatalf("settings should carry the row on its defaults:\n%s", view)
	}
	m.services.keys = m.services.keys.With(keybind.Editor, bindingOf(t))
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "editor off") {
		t.Fatalf("one moved key should be named:\n%s", view)
	}
	m.services.listKeys = m.services.listKeys.With(keybind.NewSession, bindingOf(t, "N"))
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "editor off · new_session N") {
		t.Fatalf("two moved keys should both be named:\n%s", view)
	}
	m.services.listKeys = m.services.listKeys.With(keybind.Quit, bindingOf(t, "Q"))
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "3 moved") {
		t.Fatalf("past two the row counts:\n%s", view)
	}
}

func TestListPickerMovesAKeyAndTheListFollows(t *testing.T) {
	m := keyPickerModel(t)
	view := ansi.Strip(m.viewSettings())
	for _, want := range []string{"Keybindings", "inside a session", "detach", "in the manager", "new_session", "esc and ctrl+c stay as they are"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list picker is missing %q:\n%s", want, view)
		}
	}
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.NewSession)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("N"))
	if got := m.settingsCard().State().Tables[1].Binding(keybind.NewSession).Label(); got != "N" {
		t.Fatalf("new_session = %q, want N", got)
	}
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("saving the list table should enqueue the save work")
	}
	if row := storedKeyRow(t, m, keybind.ScopeList); row != "" {
		t.Fatalf("the store was written on the update path: %s", row)
	}
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	stored, err := m.services.store.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if got := stored.Binding(keybind.NewSession).Label(); got != "N" {
		t.Fatalf("stored new_session = %q, want N", got)
	}
	if row := storedKeyRow(t, m, keybind.ScopeSession); row != "" {
		t.Fatalf("the session table was not touched and should not be stored: %s", row)
	}

	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsOpen() {
		t.Fatalf("mode after leaving settings = %v", m.screenID())
	}
	m.pressInPicker(t, runeKey("n"))
	if formOpen(m) {
		t.Fatal("n should no longer open the new-session form")
	}
	m.pressInPicker(t, runeKey("N"))
	if !formOpen(m) {
		t.Fatalf("N should open the new-session form, screen = %v", m.screenID())
	}
}

func TestListPickerRefusesWhatWouldStrandTheUser(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.Settings)
	m.pressInPicker(t, runeKey("d"))
	if !strings.Contains(m.errBar.text, "settings needs at least one key") {
		t.Fatalf("err = %q, want the settings rule", m.errBar.text)
	}
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.Kill)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("n"))
	if !strings.Contains(m.errBar.text, "n is bound to both new_session and kill") {
		t.Fatalf("err = %q, want the shared-key reason", m.errBar.text)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.settingsCard().State().Tables[1].Equal(keybind.DefaultList()) {
		t.Fatal("a refused or cancelled capture should leave the table alone")
	}
}

// Thirty-odd rows do not fit a short terminal: the picker shows a window
// around the cursor and says how many rows lie beyond it.
func TestListPickerScrollsAroundTheCursor(t *testing.T) {
	m := keyPickerModel(t)
	m.layout.height = 20
	below, above := regexp.MustCompile(`↓ \d+ more`), regexp.MustCompile(`↑ \d+ more`)
	top := ansi.Strip(m.viewSettings())
	if !below.MatchString(top) || above.MatchString(top) {
		t.Fatalf("at the top only the rows below should be counted:\n%s", top)
	}
	m.settingsCard().State().KeyCursor = len(uisettings.KeyRows(m.settingsCard().State().Tables)) - 1
	bottom := ansi.Strip(m.viewSettings())
	if !strings.Contains(bottom, "quit") || !above.MatchString(bottom) || below.MatchString(bottom) {
		t.Fatalf("at the bottom the last row shows and only the rows above are counted:\n%s", bottom)
	}
}

func listRow(t *testing.T, m *Model, name string) int {
	t.Helper()
	for i, row := range uisettings.KeyRows(m.settingsCard().State().Tables) {
		if row.Table == 1 && row.Action.Name == name {
			return i
		}
	}
	t.Fatalf("no list action %q", name)
	return -1
}

// The picker sets tmux_prefix like a session key and refuses a third prefix key.
func TestKeyPickerSetsTheTmuxPrefix(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 3
	if row := m.settingsCard().State().PickedRow(); row.Action.Name != keybind.TmuxPrefix {
		t.Fatalf("row 3 = %q, want tmux_prefix", row.Action.Name)
	}
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "off, your prefix stays") {
		t.Fatalf("an unset tmux_prefix should say tmux keeps its prefix:\n%s", view)
	}

	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyCtrlB})
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF12})
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyCtrlG})
	if !strings.Contains(m.errBar.text, "takes one key or two") {
		t.Fatalf("err = %q, want the two-key rule", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.TmuxPrefix).Label(); got != "ctrl+b / f12" {
		t.Fatalf("tmux_prefix = %q, want both keys", got)
	}

	// The refused third key's message belongs to the picker; the save
	// itself must report nothing.
	m.errBar.text = ""
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("saving should enqueue the save work")
	}
	// The completion's refresh command carries the tmux rebind; run it the
	// way the event loop would.
	updated, next := m.Update(cmd())
	*m = *updated.(*Model)
	m.applyTestMsg(t, next())
	m.drainEffects(t)
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	if got := m.services.keys.Binding(keybind.TmuxPrefix).Label(); got != "ctrl+b / f12" {
		t.Fatalf("model tmux_prefix = %q", got)
	}
	if got := storedSessionKeys(t, m).Binding(keybind.TmuxPrefix).Label(); got != "ctrl+b / f12" {
		t.Fatalf("stored tmux_prefix = %q, want both keys", got)
	}
}

// Reset puts tmux_prefix back to off, and the question says so in words.
func TestKeyPickerResetNamesTheTmuxPrefixGoingOff(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().Tables[0] = m.settingsCard().State().Tables[0].With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b"))
	m.pressInPicker(t, runeKey("r"))
	if ask := ansi.Strip(m.viewSettings()); !strings.Contains(ask, "tmux_prefix: ctrl+b back to off") {
		t.Fatalf("the question should name tmux_prefix going off:\n%s", ask)
	}
}

func TestKeySaveDefersDiskWritesToTheEffect(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	before := savedKeys(t, m)
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("a changed table should enqueue the save work")
	}
	if got := savedKeys(t, m); got != before {
		t.Fatalf("the key table was written on the update path:\n%s", got)
	}
	m.applyCmd(t, cmd)
	if saved := savedKeys(t, m); !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the store should carry the new key:\n%s", saved)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("runtime detach = %q, want f9", got)
	}
}

func TestKeySavePersistsCapturedTablesNotLaterEdits(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.effects.Main.Active == nil {
		t.Fatal("the save should be accepted before a later edit")
	}
	m.settingsCard().State().KeyCursor = 1 // review
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("t")) // ctrl+t, while the save is pending
	m.applyCmd(t, cmd)
	saved := savedKeys(t, m)
	if !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the captured detach save is missing:\n%s", saved)
	}
	if strings.Contains(saved, `"review":["ctrl+t"]`) {
		t.Fatalf("a later edit reached the captured save:\n%s", saved)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("runtime detach = %q, want f9", got)
	}
	if got := m.services.keys.Binding(keybind.Review).Label(); got != "ctrl+r" {
		t.Fatalf("runtime review = %q, want the captured default", got)
	}
}

func TestKeySavePartialFailureCommitsListOnly(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.NewSession)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("N"))
	m.settingsCard().State().KeyCursor = 0
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	original := m.services.store.SetKeys
	failOnce := true
	m.services.saveKeys = func(keys keybind.Table) error {
		if keys.Scope() == keybind.ScopeSession && failOnce {
			failOnce = false
			return errors.New("scripted session write failure")
		}
		return original(keys)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	saved := savedKeys(t, m)
	if !strings.Contains(saved, `"new_session":["N"]`) {
		t.Fatalf("the committed list table is missing:\n%s", saved)
	}
	if strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the failed session table reached the store:\n%s", saved)
	}
	if got := m.services.listKeys.Binding(keybind.NewSession).Label(); got != "N" {
		t.Fatalf("runtime list new_session = %q, want N", got)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("runtime detach changed after a failed save: %q", got)
	}
	if m.errBar.text == "" {
		t.Fatal("the failed write should report its reason")
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("the picker should keep the uncommitted table: %q", got)
	}
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().KeyPicker {
		t.Fatalf("reopening the picker failed: %q", m.errBar.text)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	cmd = m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if saved := savedKeys(t, m); !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the resubmission should finish the save:\n%s", saved)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("runtime detach = %q after the resubmission, want f9", got)
	}
}

func TestBlockedKeySaveLeavesNavigationAndResizeRunning(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	original := m.services.store.SetKeys
	started := make(chan struct{})
	release := make(chan struct{})
	m.services.saveKeys = func(keys keybind.Table) error {
		close(started)
		<-release
		return original(keys)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("the save should be accepted before the worker blocks")
	}
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-started
	done := make(chan struct{})
	go func() {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("navigation or resize waited on the running key save")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)
	if saved := savedKeys(t, m); !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the store should carry the new key once the writer unblocks:\n%s", saved)
	}
	if m.layout.width != 150 || m.layout.height != 45 {
		t.Fatalf("resize lost behind the blocked save: %dx%d", m.layout.width, m.layout.height)
	}
}

func TestKeySaveWithoutChangesQueuesNothing(t *testing.T) {
	m := keyPickerModel(t)
	if cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("an unchanged table should not enqueue save work")
	}
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 0 {
		t.Fatal("an unchanged table left a job in the lane")
	}
}

func TestKeySaveCapturesTheInstanceWriter(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	calls := 0
	m.services.saveKeys = func(keys keybind.Table) error { calls++; return m.services.store.SetKeys(keys) }
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.services.saveKeys = func(keybind.Table) error { t.Fatal("accepted job borrowed a newer writer"); return nil }
	m.applyCmd(t, cmd)
	if calls != 1 {
		t.Fatalf("captured writer calls=%d", calls)
	}
}

func TestKeySaveCanRestoreOriginalBindingBehindPendingSave(t *testing.T) {
	m := keyPickerModel(t)
	original := m.services.keys
	m.settingsCard().State().Tables[0] = original.With(keybind.Detach, bindingOf(t, "f9"))
	m.saveSettingsKeys(m.settingsCard().State())
	m.settingsCard().State().Tables[0] = original
	m.saveSettingsKeys(m.settingsCard().State())
	m.drainEffects(t)
	if !m.services.keys.Equal(original) {
		t.Fatal("pending save overwrote the user's later return to the original binding")
	}
}

// savedKeys is both stored key rows, so a test reads what a save committed.
func savedKeys(t *testing.T, m *Model) string {
	t.Helper()
	return storedKeyRow(t, m, keybind.ScopeSession) + "\n" + storedKeyRow(t, m, keybind.ScopeList)
}

func TestReopenedKeyPickerKeepsPendingSave(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	first := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, first)
	m.drainEffects(t)
	if got := storedSessionKeys(t, m).Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("reopening and closing the picker reverted the accepted binding to %q", got)
	}
}
