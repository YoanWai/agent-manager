package theme

import (
	"math"
	"testing"
)

func relativeLuminance(hex string) float64 {
	red, green, blue := HexRGB(hex)
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

// lightThemeNames mirrors TestLightThemesPresent: the themes whose backdrop
// is light, which the luminance split must classify exactly.
var lightThemeNames = map[string]bool{
	"solarized light":  true,
	"catppuccin latte": true,
	"tokyo night day":  true,
	"gruvbox light":    true,
	"rosé pine dawn":   true,
	"paper":            true,
	"kanagawa lotus":   true,
}

func TestLightBackdropClassification(t *testing.T) {
	for _, theme := range Palettes {
		if got, want := theme.LightBackdrop(), lightThemeNames[theme.Name]; got != want {
			t.Errorf("%s: LightBackdrop() = %v, want %v (Bg %s)", theme.Name, got, want, theme.Bg)
		}
	}
}

func TestLightThemesPresent(t *testing.T) {
	for _, name := range []string{
		"solarized light",
		"catppuccin latte",
		"tokyo night day",
		"gruvbox light",
		"rosé pine dawn",
		"paper",
		"kanagawa lotus",
	} {
		if Palettes[Index(name)].Name != name {
			t.Errorf("theme %q missing from the built-in set", name)
		}
	}
}

// TestThemeCounterpartsPair keeps every pairing two-sided: a counterpart
// must exist, sit on the opposite polarity, and name the theme back.
func TestThemeCounterpartsPair(t *testing.T) {
	byName := map[string]Theme{}
	for _, theme := range Palettes {
		byName[theme.Name] = theme
	}
	for _, theme := range Palettes {
		if theme.Counterpart == "" {
			continue
		}
		other, ok := byName[theme.Counterpart]
		if !ok {
			t.Errorf("%s: counterpart %q is not a built-in theme", theme.Name, theme.Counterpart)
			continue
		}
		if other.LightBackdrop() == theme.LightBackdrop() {
			t.Errorf("%s: counterpart %q sits on the same side", theme.Name, other.Name)
		}
		if other.Counterpart != theme.Name {
			t.Errorf("%s: counterpart %q points back to %q", theme.Name, other.Name, other.Counterpart)
		}
	}
}

func TestThemeTextContrast(t *testing.T) {
	for _, theme := range Palettes {
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
