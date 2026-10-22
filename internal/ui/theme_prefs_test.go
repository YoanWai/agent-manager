package ui

import (
	"github.com/YoanWai/agent-manager/internal/systheme"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"testing"
)

func TestAutoThemeName(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		scheme systheme.Scheme
		want   string
	}{
		{"unknown keeps stored", "nord", systheme.SchemeUnknown, "nord"},
		{"dark scheme keeps a dark stored theme", "nord", systheme.SchemeDark, "nord"},
		{"light scheme keeps a light stored theme", "paper", systheme.SchemeLight, "paper"},
		{"light scheme flips a dark stored theme", "nord", systheme.SchemeLight, "solarized light"},
		{"dark scheme flips a light stored theme", "paper", systheme.SchemeDark, "classic"},
		{"unset stored resolves through the default", "", systheme.SchemeLight, "solarized light"},
		{"light scheme flips a dark theme to its counterpart", "catppuccin mocha", systheme.SchemeLight, "catppuccin latte"},
		{"dark scheme flips a light theme to its counterpart", "kanagawa lotus", systheme.SchemeDark, "kanagawa wave"},
		{"dark scheme flips solarized light to solarized dark", "solarized light", systheme.SchemeDark, "solarized dark"},
		{"light scheme keeps a light theme that has a counterpart", "catppuccin latte", systheme.SchemeLight, "catppuccin latte"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoThemeName(tt.stored, tt.scheme); got != tt.want {
				t.Errorf("autoThemeName(%q, %v) = %q, want %q", tt.stored, tt.scheme, got, tt.want)
			}
		})
	}
}

func TestThemeAutoPersistsWithoutClobberingManualTheme(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if m.settings.dialog.ThemeAuto {
		t.Fatal("theme auto should default off")
	}
	m.settings.dialog.Field = uisettings.FieldThemeAuto
	m.cycleSetting(1)
	if !m.settings.dialog.ThemeAuto {
		t.Fatal("toggle should enable theme auto")
	}
	m.applyCmd(t, m.captureSettingsSave(true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != "nord" {
		t.Fatalf("manual theme clobbered by auto: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "on" {
		t.Fatalf("theme_auto not persisted: %q", got)
	}
	if !themeAutoEnabled(m.services.store) {
		t.Fatal("themeAutoEnabled should read the persisted toggle")
	}
}

func TestManualThemeCycleDisablesAuto(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "classic"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(uisettings.ThemeAutoSetting, "on"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if !m.settings.dialog.ThemeAuto {
		t.Fatal("open should reflect the persisted auto toggle")
	}
	m.settings.dialog.Field = uisettings.FieldTheme
	m.cycleSetting(1)
	if m.settings.dialog.ThemeAuto {
		t.Fatal("stepping the theme by hand should turn auto off")
	}
	m.applyCmd(t, m.captureSettingsSave(true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "off" {
		t.Fatalf("theme_auto should persist off after a manual step: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != themes[m.settings.dialog.ThemeIndex].Name {
		t.Fatalf("manual step not persisted: %q", got)
	}
}

func TestResolveStartupThemeWithoutAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	if got := resolveStartupTheme(m.services.store); got != "nord" {
		t.Fatalf("resolveStartupTheme = %q, want the stored theme", got)
	}
}
