package ui

import (
	"strings"
	"testing"
)

func TestSettingsShowsVersion(t *testing.T) {
	m := &Model{
		version:  "v0.9.0",
		settings: settingsState{toolNames: []string{"claude"}},
	}
	out := m.viewSettings()
	if !strings.Contains(out, "version") || !strings.Contains(out, "v0.9.0") {
		t.Errorf("settings missing version: %q", out)
	}
	if strings.Contains(out, "available") {
		t.Errorf("no badge expected when up to date: %q", out)
	}
	m.updateLatest = "v0.9.1"
	if out := m.viewSettings(); !strings.Contains(out, "v0.9.1") || !strings.Contains(out, "available") {
		t.Errorf("settings missing update badge: %q", out)
	}
}
