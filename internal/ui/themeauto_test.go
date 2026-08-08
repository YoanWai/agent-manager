package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/systheme"
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
	if err := m.store.SetSetting(themeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	m.openSettings()
	if m.settings.themeAuto {
		t.Fatal("theme auto should default off")
	}
	m.settings.field = settingsFieldThemeAuto
	m.cycleSetting(1)
	if !m.settings.themeAuto {
		t.Fatal("toggle should enable theme auto")
	}
	m.persistSettings()
	if got, _ := m.store.Setting(themeSetting); got != "nord" {
		t.Fatalf("manual theme clobbered by auto: %q", got)
	}
	if got, _ := m.store.Setting(themeAutoSetting); got != "on" {
		t.Fatalf("theme_auto not persisted: %q", got)
	}
	if !themeAutoEnabled(m.store) {
		t.Fatal("themeAutoEnabled should read the persisted toggle")
	}
}

func TestManualThemeCycleDisablesAuto(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	if err := m.store.SetSetting(themeSetting, "classic"); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetSetting(themeAutoSetting, "on"); err != nil {
		t.Fatal(err)
	}
	m.openSettings()
	if !m.settings.themeAuto {
		t.Fatal("open should reflect the persisted auto toggle")
	}
	m.settings.field = settingsFieldTheme
	m.cycleSetting(1)
	if m.settings.themeAuto {
		t.Fatal("stepping the theme by hand should turn auto off")
	}
	m.persistSettings()
	if got, _ := m.store.Setting(themeAutoSetting); got != "off" {
		t.Fatalf("theme_auto should persist off after a manual step: %q", got)
	}
	if got, _ := m.store.Setting(themeSetting); got != themes[m.settings.themeIndex].Name {
		t.Fatalf("manual step not persisted: %q", got)
	}
}

func TestResolveStartupThemeWithoutAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(themeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	if got := resolveStartupTheme(m.store); got != "nord" {
		t.Fatalf("resolveStartupTheme = %q, want the stored theme", got)
	}
}
