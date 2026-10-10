package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestDefaultToolFallsBackWhenSettingStale(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetDefaultTool("deleted-tool"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.applyTestMsg(t, m.openForm()())
	if got := m.form.toolNames[m.form.toolIndex]; got != "claude" {
		t.Fatalf("form default tool = %q want claude (alphabetical fallback)", got)
	}
}

func TestDefaultSplitLayout(t *testing.T) {
	m := buildModel(t)
	if !m.defaultSplitLayout() {
		t.Fatal("split should be the default layout")
	}
	if err := m.services.store.SetSetting(diffLayoutSetting, "unified"); err != nil {
		t.Fatal(err)
	}
	if m.defaultSplitLayout() {
		t.Fatal("stored unified choice should opt out of split")
	}
}

func TestSettingsTogglesQuickClose(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.dialog.quickCloseSend {
		t.Fatal("settings should open on stay-open by default")
	}
	for i := 0; i < settingsFieldQuickClose; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.dialog.field != settingsFieldQuickClose {
		t.Fatalf("stepping down should reach the quick send field, got %d", m.settings.dialog.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(quickCloseSetting); err != nil || chosen != "close" {
		t.Fatalf("close choice = %q err %v, want persisted close", chosen, err)
	}
}

func TestSettingsTogglesReviewLayout(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settings.dialog.layoutSplit {
		t.Fatal("settings should open on split by default")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.settings.dialog.field != settingsFieldTheme {
		t.Fatalf("first down should focus theme field, got %d", m.settings.dialog.field)
	}
	for i := settingsFieldTheme; i < settingsFieldLayout; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.dialog.field != settingsFieldLayout {
		t.Fatalf("stepping down should reach the layout field, got %d", m.settings.dialog.field)
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
	for m.settings.dialog.field != settingsFieldWorktree {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(worktreeSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
	}
}

func TestSettingsCoordinationBriefsTheNextSpawn(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.dialog.proactive {
		t.Fatal("coordination should open on request by default")
	}
	if !strings.Contains(ansi.Strip(m.viewSettings()), "coordination") || !strings.Contains(ansi.Strip(m.viewSettings()), "on request") {
		t.Fatalf("settings do not show the coordination row:\n%s", ansi.Strip(m.viewSettings()))
	}
	for m.settings.dialog.field != settingsFieldCoordination {
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
	if !m.settings.dialog.notifications {
		t.Fatal("notifications should open on by default")
	}
	if m.settings.dialog.notifyFinished {
		t.Fatal("notify on finish should open off by default")
	}
	for m.settings.dialog.field != settingsFieldNotify {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if chosen, err := m.services.store.Setting(notificationsSetting); err != nil || chosen != "off" {
		t.Fatalf("want stored off, got %q err %v", chosen, err)
	}
	if chosen, err := m.services.store.Setting(notifyFinishedSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
	}
}

func TestSettingsMouseTogglePersists(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.dialog.mouseDisabled {
		t.Fatal("mouse should open on by default")
	}
	for m.settings.dialog.field != settingsFieldMouse {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	_, cmd := m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if chosen, err := m.services.store.Setting(mouseSetting); err != nil || chosen != "off" {
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
		update:   updateInfo{version: "v0.9.0"},
		settings: settingsFeature{dialog: settingsState{toolNames: []string{"claude"}}},
	}
	out := m.viewSettings()
	if !strings.Contains(out, "version") || !strings.Contains(out, "v0.9.0") {
		t.Errorf("settings missing version: %q", out)
	}
	if strings.Contains(out, "update to") {
		t.Errorf("no update action expected when up to date: %q", out)
	}
	m.settings.dialog.field = settingsFieldUpdate
	out = m.viewSettings()
	if !strings.Contains(out, keyCap("↵/esc", "save")) {
		t.Errorf("current version row should hint save, not update: %q", out)
	}
	m.settings.dialog.field = 0
	m.update.latest = "v0.9.1"
	out = m.viewSettings()
	if !strings.Contains(out, "v0.9.1") || !strings.Contains(out, "update to") {
		t.Errorf("settings missing update action: %q", out)
	}
	m.settings.dialog.field = settingsFieldUpdate
	out = m.viewSettings()
	if !strings.Contains(out, keyStyle.Render("↵")+mutedStyle.Render(" update to")) {
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
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.settings.dialog.field = settingsFieldDocs
	_, cmd := m.handleSettingsKey(key("enter"))
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("enter should open the docs, got %q", opened)
	}
	if m.mode != modeSettings {
		t.Fatal("the docs row must not close settings")
	}

	opened = ""
	m.layout.width, m.layout.height = 120, 60
	frame := preparedView(m)
	if !strings.Contains(frame, hintCap("↵", "open the docs")) {
		t.Fatal("the docs shortcut should wear the footer badge")
	}
	_, cmd = m.handleSettingsClick(m.layout.cardLeft+4, m.layout.cardTop+2+settingsFieldDocs)
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("click should open the docs, got %q", opened)
	}
}

func TestSettingsBugReportRowOpensIssue(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
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

	m.settings.dialog.field = settingsFieldBugReport
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.settings.dialog.field = settingsFieldFeatureRequest
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()
	// Keep focus off the CTA rows so the accent2 unfocused styling is what paints.
	m.settings.dialog.field = settingsFieldTool
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
		"grok":   {Command: "cat"},
	}}
	m.openSettings()
	m.settings.dialog.field = settingsFieldCLIs
	m.handleSettingsKey(key("enter"))
	if !m.settings.dialog.cliPicker {
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
	m.settings.dialog.cliCursor = 1
	m.handleSettingsKey(key(" "))
	if !m.settings.dialog.cliHidden["codex"] {
		t.Fatal("space should hide the focused CLI")
	}
	m.handleSettingsKey(key("esc"))
	m.drainEffects(t)
	if m.settings.dialog.cliPicker {
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"codex":  {Command: "cat"},
	}}
	m.openSettings()
	m.settings.openCLIPicker(m)
	m.settings.dialog.cliCursor = 0
	m.handleSettingsKey(key("enter"))
	if !m.settings.dialog.cliHidden["claude"] {
		t.Fatal("first hide should succeed")
	}
	// Only codex left; refusing further hides.
	m.settings.dialog.cliCursor = 1
	m.handleSettingsKey(key("enter"))
	if m.settings.dialog.cliHidden["codex"] {
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
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.openSettings()
	m.settings.openCLIPicker(m)
	m.settings.dialog.cliCursor = len(m.settings.dialog.cliNames)
	_, cmd := m.handleSettingsKey(key("enter"))
	if opened != "" {
		t.Fatal("browser started during Update")
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(opened, "issues/new") || !strings.Contains(opened, "enhancement") {
		t.Fatalf("request row should open a feature-request issue, got %q", opened)
	}
	if !m.settings.dialog.cliPicker {
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
	m.settings.openCLIPicker(m)
	m.settings.dialog.cliCursor = len(m.settings.dialog.cliNames)
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
	m.update.version = "v0.2.0"
	m.update.latest = "v0.3.0"
	m.openSettings()
	m.settings.dialog.field = settingsFieldUpdate

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
	completed := cmd().(effectCompletedMsg)
	if completed.err != nil {
		t.Fatalf("staged settings save: %v", completed.err)
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
	swap, ok := msg.(updateAppliedMsg)
	if !ok {
		t.Fatalf("update command returned %T", msg)
	}
	if swap.err != nil {
		t.Fatalf("apply: %v", swap.err)
	}
	if !strings.HasPrefix(applied, "v0.6.0 ") {
		t.Fatalf("applyUpdate saw %q", applied)
	}
	updated, quit := m.Update(swap)
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.3.0"
	m.openSettings()
	m.settings.dialog.field = settingsFieldUpdate

	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	called := false
	applyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	m.handleSettingsKey(key("enter"))
	m.drainEffects(t)
	if called || m.update.applying {
		t.Fatal("enter on version when up to date must not start an update")
	}
	if m.mode != modeList {
		t.Fatal("enter with no update should still save and close")
	}
}

func TestSettingsUpdateRowApplyFailureSurfaces(t *testing.T) {
	m := footModel(t)
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.2.0"
	m.update.latest = "v0.3.0"
	m.openSettings()
	m.settings.dialog.field = settingsFieldUpdate

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
	completed := cmd().(effectCompletedMsg)
	if completed.err != nil {
		t.Fatalf("staged settings save: %v", completed.err)
	}
	updated, next := m.Update(completed)
	m = updated.(*Model)
	if next == nil {
		t.Fatal("no update command after the save completed")
	}
	swap, ok := next().(updateAppliedMsg)
	if !ok {
		t.Fatalf("update command returned %T", next())
	}
	updated, _ = m.Update(swap)
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
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.update.version = "v0.2.0"
	m.update.latest = "v0.3.0"
	m.openSettings()
	m.settings.dialog.themeIndex = (m.settings.dialog.themeIndex + 1) % len(themes)
	staged := themes[m.settings.dialog.themeIndex].Name
	m.settings.dialog.field = settingsFieldUpdate

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

	_, cmd := m.handleSettingsKey(key("enter"))
	if cmd == nil {
		t.Fatal("enter on the update row should start the update")
	}
	m.applyCmd(t, cmd)
	got, err := m.services.store.Setting(themeSetting)
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
	for _, theme := range themes {
		if theme.Name != current.Name {
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
			m.settings.dialog.field = settingsFieldWorktree
			m.settings.cycleSetting(m, 1)

			theirs := otherWriterTheme(t)
			if err := m.services.store.SetSetting(themeSetting, theirs); err != nil {
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

			if got, err := m.services.store.Setting(worktreeSetting); err != nil || got != "on" {
				t.Fatalf("worktree=%q err %v, want the changed value on", got, err)
			}
			if got, err := m.services.store.Setting(themeSetting); err != nil || got != theirs {
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
	if m.mode != modeList {
		t.Fatalf("esc should close the dialog, mode=%v", m.mode)
	}
	m.applyCmd(t, save)

	values, hidden, hiddenErr := settingsReadback(storeSettingWriter{st: m.services.store})
	for _, value := range values {
		raw, err := m.services.store.Setting(value.key)
		if err != nil || raw != "" {
			t.Errorf("%s=%q err %v, want unwritten", value.key, raw, err)
		}
	}
	if hiddenErr != nil || hidden != "" {
		t.Errorf("%s=%q err %v, want unwritten", hiddenToolsSetting, hidden, hiddenErr)
	}
}
