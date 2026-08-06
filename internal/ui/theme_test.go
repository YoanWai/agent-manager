package ui

import (
	"math"
	"testing"
)

// tmux forwards a passthrough payload after undoubling ESC bytes; a
// sequence wrapped without the doubling reaches the terminal truncated at
// its own first ESC.
func TestTmuxPassthroughDoublesEscapes(t *testing.T) {
	got := tmuxPassthrough("\x1b]11;#0f1115\x07")
	want := "\x1bPtmux;\x1b\x1b]11;#0f1115\x07\x1b\\"
	if got != want {
		t.Fatalf("wrapped = %q, want %q", got, want)
	}
}

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
		// Status washes sit under bold large text. Softening Errored/Accent
		// into Bg reduces contrast slightly; keep the tertiary floor (2.0)
		// so a wash that collapses toward the ink still fails the suite.
		const washMin = 2.0
		applyTheme(theme)
		if ratio := contrastRatio(theme.Errored, errWashHex()); ratio < washMin {
			t.Errorf("%s: Errored on err wash contrast %.2f, want >= %.1f",
				theme.Name, ratio, washMin)
		}
		if ratio := contrastRatio(theme.Accent, focusWashHex()); ratio < washMin {
			t.Errorf("%s: Accent on focus wash contrast %.2f, want >= %.1f",
				theme.Name, ratio, washMin)
		}
	}
}
