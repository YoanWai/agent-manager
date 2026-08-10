package ui

import (
	"strings"
	"testing"
)

// lightThemeNames mirrors TestLightThemesPresent: the themes whose backdrop
// is light, which the luminance split must classify exactly.
var lightThemeNames = map[string]bool{
	"solarized light":  true,
	"catppuccin latte": true,
	"tokyo night day":  true,
	"gruvbox light":    true,
	"rosé pine dawn":   true,
	"paper":            true,
}

func TestLightBackdropClassification(t *testing.T) {
	for _, theme := range themes {
		if got, want := theme.lightBackdrop(), lightThemeNames[theme.Name]; got != want {
			t.Errorf("%s: lightBackdrop() = %v, want %v (Bg %s)", theme.Name, got, want, theme.Bg)
		}
	}
}

func TestSGRDropsColors(t *testing.T) {
	tests := []struct {
		params string
		fg, bg bool
	}{
		{"", true, true},
		{"0", true, true},
		{"39", true, false},
		{"49", false, true},
		{"0;33", false, true},
		{"0;31", false, true},
		{"0;41", true, false},
		{"0;31;41", false, false},
		{"1;31", false, false},
		{"7", false, false},
		{"38;5;0", false, false},
		{"48;5;249", false, false},
		{"38;2;0;0;0", false, false},
		{"48;2;15;17;21", false, false},
		{"38:2::0:0:0", false, false},
		{"38;2;0;0;0;49", false, true},
		{"48;5;1;39", true, false},
		{"38;5;1;49", false, true},
		{"39;31", false, false},
		{"1;38;5;39", false, false},
		{"38;5;39", false, false},
	}
	for _, tt := range tests {
		fg, bg := sgrDropsColors(tt.params)
		if fg != tt.fg || bg != tt.bg {
			t.Errorf("sgrDropsColors(%q) = (%v, %v), want (%v, %v)", tt.params, fg, bg, tt.fg, tt.bg)
		}
	}
}

func onLightTheme(t *testing.T) {
	t.Helper()
	applyTheme(themes[themeIndex("solarized light")])
	t.Cleanup(func() { applyTheme(themes[0]) })
	if !captureOnDark {
		t.Fatal("captureOnDark not active on solarized light")
	}
}

func TestReassertCaptureColors(t *testing.T) {
	onLightTheme(t)
	got := reassertCaptureColors("\x1b[31mred\x1b[0m plain")
	if !strings.Contains(got, "\x1b[0m"+captureOpen) {
		t.Errorf("reset not followed by capture colors: %q", got)
	}
	kept := "\x1b[38;2;0;0;0mblack"
	if got := reassertCaptureColors(kept); got != kept {
		t.Errorf("explicit color rewritten: %q", got)
	}
	// A reset combined with an explicit color keeps that color: only the
	// component left at default moves onto the backdrop.
	if got := reassertCaptureColors("\x1b[0;31mred"); got != "\x1b[0;31m"+captureBgSeq+"red" {
		t.Errorf("combined reset+fg reasserted wrong components: %q", got)
	}
	if got := reassertCaptureColors("\x1b[48;5;1;39mtext"); got != "\x1b[48;5;1;39m"+captureFgSeq+"text" {
		t.Errorf("explicit bg with default fg reasserted wrong components: %q", got)
	}
}

func TestCaptureBlankRow(t *testing.T) {
	onLightTheme(t)
	row := captureBlankRow(6)
	if !strings.HasPrefix(row, captureBgSeq) || !strings.HasSuffix(row, "\x1b[0m") {
		t.Errorf("blank row not wrapped in backdrop: %q", row)
	}
	if !strings.Contains(row, strings.Repeat(" ", 6)) {
		t.Errorf("blank row not full width: %q", row)
	}
	if captureBlankRow(0) != "" {
		t.Error("zero width blank row not empty")
	}
}

// The theme handed to the panes is the color the captures are painted on,
// which is the pinned backdrop under a light theme and the theme's own
// backdrop under a dark one — never the light chrome the manager wears.
func TestAgentPaneTheme(t *testing.T) {
	onLightTheme(t)
	if got := agentPaneTheme().Background; got != themes[0].Bg {
		t.Errorf("light theme pane background = %q, want the pinned backdrop %q", got, themes[0].Bg)
	}

	dark := themes[themeIndex("tokyo night")]
	applyTheme(dark)
	if got := agentPaneTheme().Background; got != dark.Bg {
		t.Errorf("dark theme pane background = %q, want %q", got, dark.Bg)
	}
	if got := agentPaneTheme().ColorFgBg; got != "15;0" {
		t.Errorf("COLORFGBG = %q, want light-on-dark %q", got, "15;0")
	}
}
