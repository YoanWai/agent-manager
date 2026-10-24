package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

func TestThemeAutoPersistsWithoutClobberingManualTheme(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if m.settingsCard().State().ThemeAuto {
		t.Fatal("theme auto should default off")
	}
	m.settingsCard().State().Field = uisettings.FieldThemeAuto
	m.cycleSetting(1)
	if !m.settingsCard().State().ThemeAuto {
		t.Fatal("toggle should enable theme auto")
	}
	m.applyCmd(t, m.captureSettingsSave(m.settingsCard().State(), true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != "nord" {
		t.Fatalf("manual theme clobbered by auto: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "on" {
		t.Fatalf("theme_auto not persisted: %q", got)
	}
	if !uitheme.AutoEnabled(m.services.store) {
		t.Fatal("AutoEnabled should read the persisted toggle")
	}
}

func TestManualThemeCycleDisablesAuto(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "classic"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(uisettings.ThemeAutoSetting, "on"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if !m.settingsCard().State().ThemeAuto {
		t.Fatal("open should reflect the persisted auto toggle")
	}
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	if m.settingsCard().State().ThemeAuto {
		t.Fatal("stepping the theme by hand should turn auto off")
	}
	m.applyCmd(t, m.captureSettingsSave(m.settingsCard().State(), true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "off" {
		t.Fatalf("theme_auto should persist off after a manual step: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name {
		t.Fatalf("manual step not persisted: %q", got)
	}
}
