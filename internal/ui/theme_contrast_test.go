package ui

import (
	"math"
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
	}
}
