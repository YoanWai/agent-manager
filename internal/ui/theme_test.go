package ui

import (
	"math"
	"strings"
	"testing"
)

func relativeLuminance(hex string) float64 {
	red, green, blue := hexRGB(hex)
	channel := func(value int) float64 {
		scaled := float64(value) / 255
		if scaled <= 0.03928 {
			return scaled / 12.92
		}
		return math.Pow((scaled+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(red) + 0.7152*channel(green) + 0.0722*channel(blue)
}

func contrastRatio(a, b string) float64 {
	lumA := relativeLuminance(a)
	lumB := relativeLuminance(b)
	if lumA < lumB {
		lumA, lumB = lumB, lumA
	}
	return (lumA + 0.05) / (lumB + 0.05)
}

func TestLightThemesPresent(t *testing.T) {
	for _, name := range []string{
		"solarized light",
		"catppuccin latte",
		"tokyo night day",
		"gruvbox light",
		"rosé pine dawn",
		"paper",
	} {
		if themes[themeIndex(name)].Name != name {
			t.Errorf("theme %q missing from the built-in set", name)
		}
	}
}

func TestThemeTextContrast(t *testing.T) {
	for _, theme := range themes {
		checks := []struct {
			token   string
			hex     string
			minimum float64
		}{
			{"Bright", theme.Bright, 4.5},
			{"Text", theme.Text, 4.0},
			{"Dim", theme.Dim, 2.8},
			{"Subtle", theme.Subtle, 2.0},
		}
		for _, check := range checks {
			if ratio := contrastRatio(check.hex, theme.Bg); ratio < check.minimum {
				t.Errorf("%s: %s %s on Bg %s contrast %.2f, want >= %.1f",
					theme.Name, check.token, check.hex, theme.Bg, ratio, check.minimum)
			}
		}
		if theme.Surface == theme.Bg {
			t.Errorf("%s: Surface equals Bg, selected rows would be invisible", theme.Name)
		}
		// Badges paint Bg as bold ink on an accent fill, so both accents
		// have to clear the large-text floor against the backdrop tone.
		for token, accent := range map[string]string{"Accent": theme.Accent, "Accent2": theme.Accent2} {
			if ratio := contrastRatio(theme.Bg, accent); ratio < 3.0 {
				t.Errorf("%s: Bg %s on %s fill %s contrast %.2f, want >= 3.0",
					theme.Name, theme.Bg, token, accent, ratio)
			}
		}
	}
}

func globalWindowStyle(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd("show-options", "-gv", "window-style").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options window-style: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// Agents resolve their own palette against the pane background, so a theme
// switch has to reach the tmux server as well as the manager's own frame.
func TestThemeSwitchPushesPaneBackground(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	// Global options only stick while a server is up, and a server with no
	// sessions exits at once, so one session holds it open.
	if err := m.tmux.Create("panebg", "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { m.tmux.Kill("panebg") })
	t.Cleanup(func() { tmuxCmd("set-option", "-gu", "window-style").Run() })

	m.openSettings()
	m.settings.field = settingsFieldTheme

	m.settings.themeIndex = themeIndex("solarized light") - 1
	if cmd := m.cycleSetting(1); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
	if m.errBar.text != "" {
		t.Fatalf("pane theme push reported %q", m.errBar.text)
	}
	if got, want := globalWindowStyle(t), "bg="+themes[0].Bg; got != want {
		t.Errorf("light theme pushed window-style %q, want the pinned backdrop %q", got, want)
	}

	m.settings.themeIndex = themeIndex("nord") - 1
	if cmd := m.cycleSetting(1); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
	if got, want := globalWindowStyle(t), "bg="+themes[themeIndex("nord")].Bg; got != want {
		t.Errorf("dark theme pushed window-style %q, want %q", got, want)
	}
}
