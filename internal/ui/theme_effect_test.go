package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

func TestResolveStartupThemeWithoutAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	if got := resolveStartupTheme(m.services.store); got != "nord" {
		t.Fatalf("resolveStartupTheme = %q, want the stored theme", got)
	}
}
