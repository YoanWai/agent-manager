package ui

import (
	"maps"
	"slices"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
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

// Every key the modal writes is one the shell can set, with a value the
// shell would accept, so the two fronts never drift apart.
func TestSettingSpecsCoverEveryKeySettingsWrites(t *testing.T) {
	m := buildModel(t)
	before, err := m.store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	m.openSettings()
	m.openCLIPicker()
	m.handleCLIPickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	after, err := m.store.Settings()
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
		if err := spec.Check(value); err != nil {
			t.Errorf("Settings wrote %s=%q: %v", key, value, err)
		}
	}
	if written == 0 {
		t.Fatal("closing Settings wrote nothing, so the test checked nothing")
	}
	for key, spec := range specs {
		if _, present := after[key]; !present {
			t.Errorf("SettingSpecs names %s (%s), which closing Settings never writes", key, spec.Row)
		}
		if spec.Row == "" {
			t.Errorf("%s has no Settings row named", key)
		}
	}
}

func TestSettingSpecsNameEveryThemeAndAgentCLI(t *testing.T) {
	specs := specByKey(t)
	var names []string
	for _, theme := range themes {
		names = append(names, theme.Name)
	}
	if !slices.Equal(specs[themeSetting].Values, names) {
		t.Fatalf("theme takes %v, want the shipped palettes %v", specs[themeSetting].Values, names)
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
		listDensitySetting:   "comfortable",
		sessionLayoutSetting: "full",
		hideHeaderSetting:    "on",
		hideStatsSetting:     "on",
		mouseSetting:         "off",
		arrowStepSetting:     "off",
		focusKeySetting:      "attach",
		baseFetchSetting:     "off",
		backgroundSetting:    "terminal",
		themeSetting:         "nord",
	}
	for key, value := range maps.All(changed) {
		if err := m.store.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.store.SetEditor("code -n"); err != nil {
		t.Fatal(err)
	}
	m.openSettings()
	if themes[m.settings.themeIndex].Name != "nord" {
		t.Fatalf("Settings opened on %q, want the shell's nord", themes[m.settings.themeIndex].Name)
	}
	if current.Name != "nord" {
		t.Fatalf("the palette stayed %q after Settings read nord", current.Name)
	}
	if m.settings.editor.line() != "code -n" {
		t.Fatalf("editor row opened on %q", m.settings.editor.line())
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	for key, want := range changed {
		if got, _ := m.store.Setting(key); got != want {
			t.Errorf("esc wrote %s=%q over the shell's %q", key, got, want)
		}
	}
	if !m.comfortableRows || !m.fullLayout || !m.hideHeader || !m.hideStats || !m.mouseDisabled || m.arrowStep || m.focusOnEnter || !m.baseFetchOff || !m.terminalBackground {
		t.Fatal("esc did not apply the shell's values to the running manager")
	}
}
