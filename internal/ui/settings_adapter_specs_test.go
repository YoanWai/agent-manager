package ui

import (
	"slices"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

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
	m.settings.dialog.Baseline = nil
	m.settings.dialog.BaselineHidden = nil
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
	for _, theme := range themes {
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
	if themes[m.settings.dialog.ThemeIndex].Name != "nord" {
		t.Fatalf("Settings opened on %q, want the shell's nord", themes[m.settings.dialog.ThemeIndex].Name)
	}
	if m.settings.dialog.GlyphMode != presentation.GlyphModeBasic || m.prefs.glyphMode != presentation.GlyphModeBasic {
		t.Fatalf("symbols opened on %q (live %q), want basic", m.settings.dialog.GlyphMode, m.prefs.glyphMode)
	}
	if current().Name != "nord" {
		t.Fatalf("the palette stayed %q after Settings read nord", current().Name)
	}
	if m.settings.dialog.Editor.Line() != "code -n" {
		t.Fatalf("editor row opened on %q", m.settings.dialog.Editor.Line())
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
